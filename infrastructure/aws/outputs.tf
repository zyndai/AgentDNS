output "boot_public_ip" {
  description = "Boot node public IP (Elastic IP)"
  value       = aws_eip.boot.public_ip
}

output "dns01_public_ip" {
  description = "DNS01 node public IP (Elastic IP)"
  value       = aws_eip.dns01.public_ip
}

output "deployer_public_ip" {
  description = "Deployer node public IP (Elastic IP)"
  value       = aws_eip.deployer.public_ip
}

output "boot_db_endpoint" {
  description = "Boot node RDS endpoint"
  value       = aws_db_instance.boot.endpoint
}

output "dns01_db_endpoint" {
  description = "DNS01 node RDS endpoint"
  value       = aws_db_instance.dns01.endpoint
}

output "vpc_id" {
  description = "VPC ID"
  value       = aws_vpc.main.id
}
