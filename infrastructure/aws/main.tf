terraform {
  required_version = ">= 1.5"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }

  backend "s3" {
    bucket         = "zynd-terraform-state"
    key            = "agent-dns/terraform.tfstate"
    region         = "us-east-1"
    dynamodb_table = "zynd-terraform-locks"
    encrypt        = true
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "agent-dns"
      Environment = var.environment
      ManagedBy   = "terraform"
    }
  }
}

# ---------------------------------------------------------------------
# VPC + Networking
# ---------------------------------------------------------------------

resource "aws_vpc" "main" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = { Name = "zynd-${var.environment}" }
}

resource "aws_internet_gateway" "main" {
  vpc_id = aws_vpc.main.id
  tags   = { Name = "zynd-${var.environment}-igw" }
}

resource "aws_subnet" "public_a" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = "${var.aws_region}a"
  map_public_ip_on_launch = true
  tags                    = { Name = "zynd-${var.environment}-public-a" }
}

resource "aws_subnet" "public_b" {
  vpc_id                  = aws_vpc.main.id
  cidr_block              = "10.0.2.0/24"
  availability_zone       = "${var.aws_region}b"
  map_public_ip_on_launch = true
  tags                    = { Name = "zynd-${var.environment}-public-b" }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.main.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.main.id
  }
  tags = { Name = "zynd-${var.environment}-public-rt" }
}

resource "aws_route_table_association" "public_a" {
  subnet_id      = aws_subnet.public_a.id
  route_table_id = aws_route_table.public.id
}

resource "aws_route_table_association" "public_b" {
  subnet_id      = aws_subnet.public_b.id
  route_table_id = aws_route_table.public.id
}

# ---------------------------------------------------------------------
# Security Groups
# ---------------------------------------------------------------------

# EC2: allow HTTP (80), HTTPS (443), mesh gossip (4001), SSH (22)
resource "aws_security_group" "registry" {
  name_prefix = "zynd-registry-${var.environment}-"
  vpc_id      = aws_vpc.main.id
  description = "Agent DNS registry nodes"

  ingress {
    description = "HTTP"
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "HTTPS"
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "Registry API"
    from_port   = 8080
    to_port     = 8080
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "Mesh gossip"
    from_port   = 4001
    to_port     = 4001
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    description = "SSH"
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = var.ssh_allowed_cidrs
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "zynd-registry-${var.environment}" }
}

# RDS: only accessible from the registry security group
resource "aws_security_group" "database" {
  name_prefix = "zynd-db-${var.environment}-"
  vpc_id      = aws_vpc.main.id
  description = "Agent DNS Postgres (RDS)"

  ingress {
    description     = "Postgres from registry nodes"
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.registry.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "zynd-db-${var.environment}" }
}

# ---------------------------------------------------------------------
# RDS Subnet Group (needs 2 AZs)
# ---------------------------------------------------------------------

resource "aws_db_subnet_group" "main" {
  name       = "zynd-${var.environment}"
  subnet_ids = [aws_subnet.public_a.id, aws_subnet.public_b.id]
  tags       = { Name = "zynd-${var.environment}-db-subnet" }
}

# ---------------------------------------------------------------------
# RDS Postgres — Boot Node DB
# ---------------------------------------------------------------------

resource "aws_db_instance" "boot" {
  identifier     = "zns-boot"
  engine         = "postgres"
  engine_version = "16.4"
  instance_class = var.db_instance_class

  allocated_storage     = 20
  max_allocated_storage = 100
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = "agentdns"
  username = var.db_username
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.database.id]
  publicly_accessible    = false
  multi_az               = false
  skip_final_snapshot    = var.environment != "prod"

  backup_retention_period = 7
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  tags = { Name = "zns-boot-db", Node = "boot" }
}

# ---------------------------------------------------------------------
# RDS Postgres — DNS01 Node DB
# ---------------------------------------------------------------------

resource "aws_db_instance" "dns01" {
  identifier     = "zns01"
  engine         = "postgres"
  engine_version = "16.4"
  instance_class = var.db_instance_class

  allocated_storage     = 20
  max_allocated_storage = 100
  storage_type          = "gp3"
  storage_encrypted     = true

  db_name  = "agentdns"
  username = var.db_username
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.main.name
  vpc_security_group_ids = [aws_security_group.database.id]
  publicly_accessible    = false
  multi_az               = false
  skip_final_snapshot    = var.environment != "prod"

  backup_retention_period = 7
  backup_window           = "03:00-04:00"
  maintenance_window      = "sun:04:00-sun:05:00"

  tags = { Name = "zns01-db", Node = "dns01" }
}

# ---------------------------------------------------------------------
# EC2 Key Pair
# ---------------------------------------------------------------------

resource "aws_key_pair" "deployer" {
  key_name   = "zynd-${var.environment}-deployer"
  public_key = var.ssh_public_key
}

# ---------------------------------------------------------------------
# EC2 — Boot Node
# ---------------------------------------------------------------------

resource "aws_instance" "boot" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.ec2_instance_type
  key_name               = aws_key_pair.deployer.key_name
  subnet_id              = aws_subnet.public_a.id
  vpc_security_group_ids = [aws_security_group.registry.id]

  root_block_device {
    volume_size = 30
    volume_type = "gp3"
    encrypted   = true
  }

  user_data = templatefile("${path.module}/user_data.sh.tpl", {
    node_name    = "boot"
    db_host      = aws_db_instance.boot.address
    db_port      = aws_db_instance.boot.port
    db_name      = "agentdns"
    db_user      = var.db_username
    db_password  = var.db_password
    peer_address = "" # boot node has no initial peer
    environment  = var.environment
  })

  tags = { Name = "zns-boot" }
}

resource "aws_eip" "boot" {
  instance = aws_instance.boot.id
  domain   = "vpc"
  tags     = { Name = "zynd-${var.environment}-boot-eip" }
}

# ---------------------------------------------------------------------
# EC2 — DNS01 Node
# ---------------------------------------------------------------------

resource "aws_instance" "dns01" {
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.ec2_instance_type
  key_name               = aws_key_pair.deployer.key_name
  subnet_id              = aws_subnet.public_a.id
  vpc_security_group_ids = [aws_security_group.registry.id]

  root_block_device {
    volume_size = 30
    volume_type = "gp3"
    encrypted   = true
  }

  user_data = templatefile("${path.module}/user_data.sh.tpl", {
    node_name    = "dns01"
    db_host      = aws_db_instance.dns01.address
    db_port      = aws_db_instance.dns01.port
    db_name      = "agentdns"
    db_user      = var.db_username
    db_password  = var.db_password
    peer_address = aws_eip.boot.public_ip
    environment  = var.environment
  })

  tags = { Name = "zns01" }

  depends_on = [aws_instance.boot]
}

resource "aws_eip" "dns01" {
  instance = aws_instance.dns01.id
  domain   = "vpc"
  tags     = { Name = "zynd-${var.environment}-dns01-eip" }
}

# ---------------------------------------------------------------------
# Data: Latest Ubuntu 22.04 AMI
# ---------------------------------------------------------------------

data "aws_ami" "ubuntu" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-amd64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

data "aws_ami" "ubuntu_arm64" {
  most_recent = true
  owners      = ["099720109477"] # Canonical

  filter {
    name   = "name"
    values = ["ubuntu/images/hvm-ssd/ubuntu-jammy-22.04-arm64-server-*"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

# ---------------------------------------------------------------------
# EC2 — Deployer Node
# ---------------------------------------------------------------------

resource "aws_instance" "deployer" {
  ami                    = data.aws_ami.ubuntu_arm64.id
  instance_type          = "t4g.large"
  key_name               = aws_key_pair.deployer.key_name
  subnet_id              = aws_subnet.public_a.id
  vpc_security_group_ids = [aws_security_group.registry.id]

  root_block_device {
    volume_size = 30
    volume_type = "gp3"
    encrypted   = true
  }

  tags = { Name = "zynd-deployer" }
}

resource "aws_eip" "deployer" {
  instance = aws_instance.deployer.id
  domain   = "vpc"
  tags     = { Name = "zynd-deployer-eip" }
}
