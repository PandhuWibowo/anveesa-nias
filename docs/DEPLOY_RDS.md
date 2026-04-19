# AWS RDS Deployment Guide

Complete guide for deploying Anveesa Nias with AWS RDS (PostgreSQL, MySQL, or MariaDB).

## 🎯 Why AWS RDS?

✅ **Fully managed** - AWS handles backups, updates, and monitoring  
✅ **High availability** - Multi-AZ deployments with automatic failover  
✅ **Automatic backups** - Point-in-time recovery  
✅ **Scalability** - Easy to upgrade instance size  
✅ **Security** - Encryption at rest and in transit  
✅ **No locking issues** - Perfect for production workloads

---

## 🚀 Quick Start

### Step 1: Create RDS Instance

Choose one of:
- **RDS PostgreSQL** (Recommended) - Best overall performance
- **RDS MySQL** - If you prefer MySQL ecosystem
- **RDS MariaDB** - Open-source MySQL alternative

### Step 2: Configure Security Group

Allow your application server to connect:

```
Type: PostgreSQL/MySQL
Protocol: TCP
Port: 5432 (PostgreSQL) or 3306 (MySQL/MariaDB)
Source: Your application server security group
```

### Step 3: Deploy Application

```bash
# Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.production -o .env

# Edit with your RDS details
nano .env

# Start
docker-compose -f docker-compose.prod.yml up -d
```

---

## 📋 RDS Connection Strings

### PostgreSQL RDS

#### Without SSL (Not Recommended)
```bash
DATABASE_URL=postgres://username:password@mydb.123456789.us-east-1.rds.amazonaws.com:5432/nias?sslmode=disable
```

#### With SSL (Recommended)
```bash
DATABASE_URL=postgres://username:password@mydb.123456789.us-east-1.rds.amazonaws.com:5432/nias?sslmode=require

# Or for full verification:
DATABASE_URL=postgres://username:password@mydb.123456789.us-east-1.rds.amazonaws.com:5432/nias?sslmode=verify-full
```

### MySQL RDS

#### Without SSL
```bash
DATABASE_URL=mysql://username:password@mydb.123456789.us-east-1.rds.amazonaws.com:3306/nias
```

#### With SSL
```bash
# Download RDS CA certificate first
curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem

# Then use:
DATABASE_URL=mysql://username:password@mydb.123456789.us-east-1.rds.amazonaws.com:3306/nias?tls=custom
DB_SSL_ROOT_CERT=/path/to/global-bundle.pem
```

### MariaDB RDS

Same as MySQL:

```bash
DATABASE_URL=mysql://username:password@mydb.123456789.us-east-1.rds.amazonaws.com:3306/nias?tls=custom
DB_SSL_ROOT_CERT=/path/to/global-bundle.pem
```

---

## 🔧 Environment Configuration

### Complete .env for RDS PostgreSQL

```bash
# Environment
NIAS_ENV=production

# Database - RDS PostgreSQL
DB_DRIVER=postgres
DATABASE_URL=postgres://admin:PASSWORD@mydb.xxxxx.us-east-1.rds.amazonaws.com:5432/nias?sslmode=require
DB_SSL_MODE=require

# Security - REQUIRED
JWT_SECRET=your-secure-jwt-secret-min-32-characters-here
NIAS_ENCRYPTION_KEY=your-32-byte-encryption-key-here
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!

# Application
PORT=8080
HOST=0.0.0.0
CORS_ORIGIN=https://yourdomain.com
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW=60
LOG_LEVEL=info
```

### Complete .env for RDS MySQL/MariaDB

```bash
# Environment
NIAS_ENV=production

# Database - RDS MySQL/MariaDB
DB_DRIVER=mysql
DATABASE_URL=mysql://admin:PASSWORD@mydb.xxxxx.us-east-1.rds.amazonaws.com:3306/nias?tls=custom&parseTime=true
DB_SSL_MODE=require
DB_SSL_ROOT_CERT=/app/certs/rds-ca-bundle.pem

# Security - REQUIRED
JWT_SECRET=your-secure-jwt-secret-min-32-characters-here
NIAS_ENCRYPTION_KEY=your-32-byte-encryption-key-here
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!

# Application
PORT=8080
HOST=0.0.0.0
CORS_ORIGIN=https://yourdomain.com
RATE_LIMIT_ENABLED=true
LOG_LEVEL=info
```

---

## 📦 Docker Deployment with RDS

### Option 1: Using docker-compose.prod.yml

Create `docker-compose.override.yml`:

```yaml
services:
  nias:
    image: anveesa/nias:latest
    environment:
      # Override database settings for RDS
      - DB_DRIVER=postgres
      - DATABASE_URL=postgres://user:pass@mydb.xxxxx.us-east-1.rds.amazonaws.com:5432/nias?sslmode=require
      - DB_SSL_MODE=require
```

Then start:

```bash
docker-compose -f docker-compose.prod.yml -f docker-compose.override.yml up -d
```

### Option 2: Environment Variables Only

```bash
docker run -d \
  -p 8080:8080 \
  -e DB_DRIVER=postgres \
  -e DATABASE_URL="postgres://user:pass@mydb.xxxxx.rds.amazonaws.com:5432/nias?sslmode=require" \
  -e JWT_SECRET="your-secret" \
  -e NIAS_ENCRYPTION_KEY="your-key" \
  -e DEFAULT_ADMIN_PASSWORD="YourPass123!" \
  anveesa/nias:latest
```

---

## 🔐 RDS Security Best Practices

### 1. Use Parameter Store / Secrets Manager

Instead of hardcoding passwords:

```bash
# Store in AWS Secrets Manager
aws secretsmanager create-secret \
  --name anveesa-nias/db-password \
  --secret-string "your-db-password"

# Retrieve in application
DATABASE_PASSWORD=$(aws secretsmanager get-secret-value \
  --secret-id anveesa-nias/db-password \
  --query SecretString \
  --output text)
```

### 2. Use IAM Database Authentication

For PostgreSQL RDS with IAM (no password needed):

```bash
# Enable IAM authentication on RDS instance
# Then use IAM role for authentication

# Application assumes IAM role, gets temporary token
# No password in environment variables!
```

### 3. Encrypt Connection with SSL

#### PostgreSQL RDS

```bash
# Download RDS certificate
curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem

# Use in connection string
DATABASE_URL=postgres://user:pass@host:5432/nias?sslmode=verify-full&sslrootcert=/path/to/global-bundle.pem
```

#### MySQL/MariaDB RDS

```bash
# Download RDS certificate
curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem

# Configure in .env
DB_SSL_ROOT_CERT=/path/to/global-bundle.pem
DATABASE_URL=mysql://user:pass@host:3306/nias?tls=custom&parseTime=true
```

### 4. Network Isolation

- Use VPC with private subnets
- Place RDS in private subnet (no public access)
- Use Security Groups to restrict access
- Application in same VPC communicates privately

---

## 🏗️ RDS Instance Setup

### Create RDS PostgreSQL

```bash
aws rds create-db-instance \
  --db-instance-identifier anveesa-nias-db \
  --db-instance-class db.t3.micro \
  --engine postgres \
  --engine-version 16.1 \
  --master-username nias_admin \
  --master-user-password "YourSecurePassword" \
  --allocated-storage 20 \
  --vpc-security-group-ids sg-xxxxx \
  --db-subnet-group-name my-subnet-group \
  --backup-retention-period 7 \
  --preferred-backup-window "03:00-04:00" \
  --preferred-maintenance-window "mon:04:00-mon:05:00" \
  --enable-iam-database-authentication \
  --storage-encrypted \
  --publicly-accessible false
```

### Create RDS MySQL

```bash
aws rds create-db-instance \
  --db-instance-identifier anveesa-nias-mysql \
  --db-instance-class db.t3.micro \
  --engine mysql \
  --engine-version 8.0.35 \
  --master-username nias_admin \
  --master-user-password "YourSecurePassword" \
  --allocated-storage 20 \
  --vpc-security-group-ids sg-xxxxx \
  --db-subnet-group-name my-subnet-group \
  --backup-retention-period 7 \
  --storage-encrypted \
  --publicly-accessible false
```

### Create Database

After RDS instance is available:

```bash
# For PostgreSQL
psql -h mydb.xxxxx.rds.amazonaws.com -U nias_admin postgres -c "CREATE DATABASE nias;"

# For MySQL
mysql -h mydb.xxxxx.rds.amazonaws.com -u nias_admin -p -e "CREATE DATABASE nias;"
```

---

## 📊 RDS Instance Sizing

### Development/Testing

- **Instance:** `db.t3.micro` or `db.t4g.micro`
- **Storage:** 20GB
- **Cost:** ~$15-20/month

### Small Production (1-10 users)

- **Instance:** `db.t3.small` or `db.t4g.small`
- **Storage:** 50GB
- **Cost:** ~$30-40/month

### Medium Production (10-50 users)

- **Instance:** `db.t3.medium` or `db.m6g.large`
- **Storage:** 100GB
- **Multi-AZ:** Yes
- **Cost:** ~$100-200/month

### Large Production (50+ users)

- **Instance:** `db.m6g.xlarge` or larger
- **Storage:** 500GB+
- **Multi-AZ:** Yes
- **Read Replicas:** Yes
- **Cost:** $500+/month

---

## 🔄 Migration to RDS

### From SQLite to RDS

#### Step 1: Export SQLite Data

```bash
# Export to SQL
sqlite3 data.db .dump > backup.sql
```

#### Step 2: Convert and Import

**For PostgreSQL RDS:**

```bash
# Clean up SQLite-specific syntax
sed -i 's/INTEGER PRIMARY KEY AUTOINCREMENT/SERIAL PRIMARY KEY/g' backup.sql
sed -i 's/AUTOINCREMENT//g' backup.sql
sed -i 's/DATETIME/TIMESTAMP/g' backup.sql

# Import to RDS
psql -h mydb.xxxxx.rds.amazonaws.com -U nias_admin -d nias < backup.sql
```

**For MySQL RDS:**

```bash
# Convert to MySQL syntax
sed -i 's/INTEGER PRIMARY KEY AUTOINCREMENT/INT PRIMARY KEY AUTO_INCREMENT/g' backup.sql
sed -i 's/AUTOINCREMENT/AUTO_INCREMENT/g' backup.sql

# Import to RDS
mysql -h mydb.xxxxx.rds.amazonaws.com -u nias_admin -p nias < backup.sql
```

#### Step 3: Update Application Config

```bash
# Update .env
DB_DRIVER=postgres  # or mysql
DATABASE_URL=postgres://user:pass@mydb.xxxxx.rds.amazonaws.com:5432/nias?sslmode=require

# Restart application
docker-compose -f docker-compose.prod.yml restart
```

---

## 📁 Docker Compose for RDS

Create `docker-compose.rds.yml`:

```yaml
services:
  nias:
    image: anveesa/nias:latest
    container_name: anveesa-nias
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      # Mount RDS certificate if needed
      - ./certs:/app/certs:ro
    environment:
      # Environment
      - NIAS_ENV=production
      - PORT=8080
      - HOST=0.0.0.0

      # Database - RDS PostgreSQL
      - DB_DRIVER=postgres
      - DATABASE_URL=${RDS_DATABASE_URL}
      - DB_SSL_MODE=require

      # Security
      - JWT_SECRET=${JWT_SECRET}
      - NIAS_ENCRYPTION_KEY=${NIAS_ENCRYPTION_KEY}
      - DEFAULT_ADMIN_PASSWORD=${DEFAULT_ADMIN_PASSWORD}

      # Application
      - CORS_ORIGIN=${CORS_ORIGIN}
      - RATE_LIMIT_ENABLED=true
      - LOG_LEVEL=info

    healthcheck:
      test: ["CMD", "wget", "--no-verbose", "--tries=1", "--spider", "http://localhost:8080/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
```

Then use:

```bash
# Set environment variables
export RDS_DATABASE_URL="postgres://user:pass@mydb.xxxxx.rds.amazonaws.com:5432/nias?sslmode=require"
export JWT_SECRET="your-secret"
export NIAS_ENCRYPTION_KEY="your-key"
export DEFAULT_ADMIN_PASSWORD="YourPass123!"

# Start
docker-compose -f docker-compose.rds.yml up -d
```

---

## 🔐 SSL/TLS Configuration

### PostgreSQL RDS SSL Modes

| Mode | Security | Verification |
|------|----------|--------------|
| `disable` | ❌ No encryption | None |
| `require` | ✅ Encrypted | Server identity not verified |
| `verify-ca` | ✅ Encrypted | Verifies server certificate |
| `verify-full` | ✅ Encrypted | Verifies server certificate + hostname |

**Recommended:** `require` or `verify-full`

### MySQL/MariaDB RDS SSL

1. **Download RDS Certificate:**

```bash
curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
```

2. **Mount in Docker:**

```yaml
volumes:
  - ./global-bundle.pem:/app/certs/rds-ca-bundle.pem:ro
```

3. **Configure:**

```bash
DATABASE_URL=mysql://user:pass@mydb.xxxxx.rds.amazonaws.com:3306/nias?tls=custom&parseTime=true
DB_SSL_ROOT_CERT=/app/certs/rds-ca-bundle.pem
```

---

## 🌍 Multi-Region Deployment

### Active-Passive (Multi-AZ)

```bash
# Enable Multi-AZ on RDS (automatic)
aws rds modify-db-instance \
  --db-instance-identifier anveesa-nias-db \
  --multi-az \
  --apply-immediately
```

**Benefits:**
- Automatic failover to standby in different AZ
- No application changes needed
- RDS endpoint stays the same

### Read Replicas (Read Scaling)

```bash
# Create read replica
aws rds create-db-instance-read-replica \
  --db-instance-identifier anveesa-nias-replica \
  --source-db-instance-identifier anveesa-nias-db \
  --db-instance-class db.t3.small

# Use replica for read-only queries (future feature)
```

---

## 🛠️ Complete .env.rds Examples

### Example 1: RDS PostgreSQL (us-east-1)

```bash
# Environment
NIAS_ENV=production
PORT=8080
HOST=0.0.0.0

# Database - RDS PostgreSQL
DB_DRIVER=postgres
DATABASE_URL=postgres://nias_admin:MySecurePass123@anveesa-nias.c9xyz12345.us-east-1.rds.amazonaws.com:5432/nias?sslmode=require
DB_SSL_MODE=require

# Security
JWT_SECRET=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6
NIAS_ENCRYPTION_KEY=a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6
DEFAULT_ADMIN_USERNAME=admin
DEFAULT_ADMIN_PASSWORD=AdminSecure123!

# Application
CORS_ORIGIN=https://db.yourdomain.com
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW=60
JWT_EXPIRY_HOURS=72
LOG_LEVEL=info
```

### Example 2: RDS MySQL with SSL (ap-southeast-1)

```bash
# Environment
NIAS_ENV=production
PORT=8080

# Database - RDS MySQL
DB_DRIVER=mysql
DATABASE_URL=mysql://nias_admin:MySecurePass123@anveesa-nias.c9xyz12345.ap-southeast-1.rds.amazonaws.com:3306/nias?tls=custom&parseTime=true&charset=utf8mb4
DB_SSL_MODE=require
DB_SSL_ROOT_CERT=/app/certs/rds-ca-bundle.pem

# Security
JWT_SECRET=your-jwt-secret-min-32-characters
NIAS_ENCRYPTION_KEY=your-32-character-encryption-key
DEFAULT_ADMIN_PASSWORD=SecurePass123!

# Application
CORS_ORIGIN=*
RATE_LIMIT_ENABLED=true
LOG_LEVEL=info
```

### Example 3: RDS MariaDB

```bash
# Same as MySQL, just different engine on RDS
DB_DRIVER=mysql
DATABASE_URL=mysql://user:pass@mydb.xxxxx.rds.amazonaws.com:3306/nias?tls=custom&parseTime=true
```

---

## 🚀 Deployment Steps

### Step 1: Create RDS Instance via AWS Console

1. Go to AWS RDS Console
2. Click "Create database"
3. Choose engine:
   - PostgreSQL 16.x (recommended)
   - MySQL 8.0.x
   - MariaDB 10.11.x
4. Template: Production or Dev/Test
5. DB instance identifier: `anveesa-nias-db`
6. Master username: `nias_admin`
7. Master password: (generate strong password)
8. Instance configuration:
   - Burstable classes: `db.t3.micro`, `db.t3.small`
   - Standard classes: `db.m6g.large`, `db.m6g.xlarge`
9. Storage:
   - Allocated storage: 20GB minimum
   - Storage autoscaling: Enable
   - Max storage: 100GB
10. Connectivity:
    - VPC: Your application VPC
    - Public access: No (recommended)
    - VPC security group: Create or select
11. Additional configuration:
    - Initial database: `nias`
    - Backup retention: 7 days
    - Enable encryption: Yes
    - Enable Enhanced Monitoring: Optional

### Step 2: Configure Security Group

```bash
# Allow application to connect
aws ec2 authorize-security-group-ingress \
  --group-id sg-xxxxx \
  --protocol tcp \
  --port 5432 \
  --source-group sg-yyyyy  # Your application security group
```

### Step 3: Get RDS Endpoint

```bash
aws rds describe-db-instances \
  --db-instance-identifier anveesa-nias-db \
  --query 'DBInstances[0].Endpoint.Address' \
  --output text

# Example output:
# anveesa-nias-db.c9xyz12345.us-east-1.rds.amazonaws.com
```

### Step 4: Create .env File

```bash
# Create .env with RDS endpoint
cat > .env <<EOF
DB_DRIVER=postgres
DATABASE_URL=postgres://nias_admin:YOUR_PASSWORD@YOUR_RDS_ENDPOINT:5432/nias?sslmode=require
JWT_SECRET=$(openssl rand -hex 32)
NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16)
DEFAULT_ADMIN_PASSWORD=SecureAdmin123!
CORS_ORIGIN=https://yourdomain.com
EOF
```

### Step 5: Deploy Application

```bash
# Using Docker
docker run -d \
  -p 8080:8080 \
  --env-file .env \
  anveesa/nias:latest

# Or using docker-compose
docker-compose -f docker-compose.prod.yml up -d
```

### Step 6: Verify Connection

```bash
# Check application health
curl http://localhost:8080/health

# Check version
curl http://localhost:8080/version

# Check logs
docker logs anveesa-nias | grep "Database initialized"

# Should see:
# Database initialized: PostgreSQL
# or
# Database initialized: MySQL/MariaDB
```

---

## 📊 Monitoring RDS

### CloudWatch Metrics

Monitor these metrics:
- **CPUUtilization** - Keep under 80%
- **DatabaseConnections** - Monitor connection count
- **FreeableMemory** - Ensure sufficient RAM
- **ReadLatency / WriteLatency** - Query performance
- **StorageSpaceUtilization** - Disk space usage

### Set Up Alarms

```bash
# CPU alarm
aws cloudwatch put-metric-alarm \
  --alarm-name anveesa-nias-high-cpu \
  --alarm-description "RDS CPU > 80%" \
  --metric-name CPUUtilization \
  --namespace AWS/RDS \
  --statistic Average \
  --period 300 \
  --evaluation-periods 2 \
  --threshold 80 \
  --comparison-operator GreaterThanThreshold \
  --dimensions Name=DBInstanceIdentifier,Value=anveesa-nias-db
```

### Enhanced Monitoring

Enable for detailed metrics:
- OS processes
- Memory usage
- Disk I/O
- Network throughput

---

## 💾 Backup Strategy

### Automated Backups (Built-in)

RDS automatically backs up:
- Daily snapshots
- Transaction logs for point-in-time recovery
- Retention: 7-35 days (configurable)

### Manual Snapshots

```bash
# Create manual snapshot
aws rds create-db-snapshot \
  --db-instance-identifier anveesa-nias-db \
  --db-snapshot-identifier anveesa-nias-manual-$(date +%Y%m%d)

# List snapshots
aws rds describe-db-snapshots \
  --db-instance-identifier anveesa-nias-db
```

### Point-in-Time Recovery

```bash
# Restore to specific time
aws rds restore-db-instance-to-point-in-time \
  --source-db-instance-identifier anveesa-nias-db \
  --target-db-instance-identifier anveesa-nias-restored \
  --restore-time 2026-04-18T12:00:00Z
```

---

## 🔧 Troubleshooting

### Cannot Connect to RDS

**Check 1: Security Group**

```bash
# Verify security group allows connection
aws ec2 describe-security-groups --group-ids sg-xxxxx
```

**Check 2: Network Connectivity**

```bash
# Test from application server
telnet mydb.xxxxx.rds.amazonaws.com 5432

# Or using nc
nc -zv mydb.xxxxx.rds.amazonaws.com 5432
```

**Check 3: Credentials**

```bash
# Test connection manually
psql -h mydb.xxxxx.rds.amazonaws.com -U nias_admin -d nias

# For MySQL
mysql -h mydb.xxxxx.rds.amazonaws.com -u nias_admin -p -D nias
```

### SSL/TLS Errors

**Error:** "x509: certificate signed by unknown authority"

**Fix:**

```bash
# Download RDS CA bundle
curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem

# Add to DATABASE_URL
DATABASE_URL=postgres://user:pass@host:5432/nias?sslmode=verify-full&sslrootcert=/path/to/global-bundle.pem
```

### Connection Pool Exhausted

**Error:** "sorry, too many clients already"

**Fix:**

Increase RDS max_connections or adjust application pool:

```bash
# Check current max_connections
psql -h mydb.xxxxx.rds.amazonaws.com -U nias_admin -d nias -c "SHOW max_connections;"

# Modify parameter group to increase max_connections
# Then restart RDS instance
```

### Slow Queries

**Check slow query log:**

```bash
# Enable slow query log in RDS parameter group
# Set: log_min_duration_statement = 1000 (PostgreSQL)
# Or: slow_query_log = 1, long_query_time = 1 (MySQL)

# View logs in CloudWatch
aws logs tail /aws/rds/instance/anveesa-nias-db/postgresql --follow
```

---

## 💰 Cost Optimization

### 1. Use ARM-Based Instances (Graviton)

```bash
# Graviton instances are 20-40% cheaper
# db.t4g.small instead of db.t3.small
# db.m6g.large instead of db.m5.large
```

### 2. Reserved Instances

- 1-year: ~40% discount
- 3-year: ~60% discount

### 3. Storage Optimization

- Use gp3 instead of gp2 (better price/performance)
- Enable storage autoscaling (pay for what you use)
- Regular cleanup of old audit logs

### 4. Right-Sizing

Monitor metrics and downsize if:
- CPU consistently under 20%
- Memory mostly unused
- Low connection count

---

## 🔒 Security Checklist

- [ ] RDS in private subnet (no public access)
- [ ] Security group restricts access to application only
- [ ] SSL/TLS enabled (sslmode=require or verify-full)
- [ ] Strong master password (20+ characters)
- [ ] Storage encryption enabled
- [ ] Automated backups enabled (7+ days retention)
- [ ] Enhanced monitoring enabled
- [ ] CloudWatch alarms configured
- [ ] Parameter groups reviewed (disable public access)
- [ ] IAM authentication enabled (optional but recommended)
- [ ] Secrets stored in AWS Secrets Manager (not .env)
- [ ] Regular security patches (automatic minor version upgrades)

---

## 📚 Additional Resources

- [AWS RDS Documentation](https://docs.aws.amazon.com/rds/)
- [RDS PostgreSQL Best Practices](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/CHAP_BestPractices.html)
- [RDS MySQL Best Practices](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/CHAP_MySQL.html)
- [RDS Security Best Practices](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/CHAP_BestPractices.Security.html)
- [RDS SSL/TLS](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.SSL.html)

---

## 🆘 Support

Issues? Check:

1. **Application logs:** `docker logs anveesa-nias`
2. **RDS status:** AWS Console → RDS → Your instance
3. **Network:** Security groups, route tables, NACLs
4. **Credentials:** Test manual connection with psql/mysql
5. **SSL:** Verify certificate paths and permissions

---

## ✅ Quick Reference

```bash
# Environment variables for RDS PostgreSQL
DB_DRIVER=postgres
DATABASE_URL=postgres://user:pass@mydb.xxxxx.rds.amazonaws.com:5432/nias?sslmode=require
DB_SSL_MODE=require

# Environment variables for RDS MySQL
DB_DRIVER=mysql
DATABASE_URL=mysql://user:pass@mydb.xxxxx.rds.amazonaws.com:3306/nias?tls=custom&parseTime=true
DB_SSL_ROOT_CERT=/app/certs/rds-ca-bundle.pem

# Test connection
psql -h mydb.xxxxx.rds.amazonaws.com -U nias_admin -d nias
mysql -h mydb.xxxxx.rds.amazonaws.com -u nias_admin -p -D nias

# Deploy
docker-compose -f docker-compose.prod.yml up -d

# Monitor
aws rds describe-db-instances --db-instance-identifier anveesa-nias-db
aws cloudwatch get-metric-statistics --namespace AWS/RDS --metric-name CPUUtilization

# Backup
aws rds create-db-snapshot --db-instance-identifier anveesa-nias-db --db-snapshot-identifier backup-$(date +%Y%m%d)
```

---

**🎉 Your application is now fully compatible with AWS RDS!**

Choose PostgreSQL RDS for best performance and compatibility.
