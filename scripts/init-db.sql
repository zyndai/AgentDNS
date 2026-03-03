-- Initialize databases for multi-node testbed
-- Each registry node gets its own database for isolation

CREATE DATABASE agentdns_b;
CREATE DATABASE agentdns_c;

-- Grant access
GRANT ALL PRIVILEGES ON DATABASE agentdns TO agentdns;
GRANT ALL PRIVILEGES ON DATABASE agentdns_b TO agentdns;
GRANT ALL PRIVILEGES ON DATABASE agentdns_c TO agentdns;
