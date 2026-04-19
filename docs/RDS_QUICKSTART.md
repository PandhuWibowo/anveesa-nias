# AWS RDS Quick Start (5 Minutes)

The fastest way to deploy Anveesa Nias with AWS RDS.

## 🚀 Quick Deploy

### Step 1: Create RDS Instance (AWS Console)

1. Go to [AWS RDS Console](https://console.aws.amazon.com/rds)
2. Click **Create database**
3. Choose:
   - **Engine**: PostgreSQL 16.x (recommended) or MySQL 8.0.x
   - **Template**: Free tier (for testing) or Production
   - **DB instance identifier**: `anveesa-nias`
   - **Master username**: `nias_admin`
   - **Master password**: (generate strong password)
   - **Instance**: `db.t3.micro` or larger
   - **Storage**: 20 GB minimum
   - **Public access**: No (use VPC)
   - **Initial database**: `nias`
4. Click **Create database**
5. Wait ~5 minutes for creation

### Step 2: Configure Security Group

1. Go to your RDS instance → **Connectivity & security**
2. Click on the **VPC security groups** link
3. Add inbound rule:
   - **Type**: PostgreSQL or MySQL
   - **Port**: 5432 (PostgreSQL) or 3306 (MySQL)
   - **Source**: Your application server security group

### Step 3: Get Connection Details

```bash
# From AWS Console, copy the "Endpoint" (looks like):
# anveesa-nias.xxxxx.us-east-1.rds.amazonaws.com
```

### Step 4: Deploy Application

```bash
# Download config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.rds-postgres
mv .env.rds-postgres .env

# Edit .env with your RDS details
nano .env
```

**Set these values in .env:**

```bash
# Replace with your actual RDS endpoint
RDS_DATABASE_URL=postgres://nias_admin:YOUR_PASSWORD@anveesa-nias.xxxxx.us-east-1.rds.amazonaws.com:5432/nias?sslmode=require

# Generate secrets
JWT_SECRET=$(openssl rand -hex 32)
NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16)
DEFAULT_ADMIN_PASSWORD=YourSecurePass123!
```

### Step 5: Start Application

```bash
# Download docker-compose
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.rds-postgres.yml

# Start
docker-compose -f docker-compose.rds-postgres.yml up -d

# Check logs
docker logs -f anveesa-nias
```

**✅ Done!** Access at `http://localhost:8080`

---

## 🔐 For MySQL/MariaDB RDS

### Additional SSL Setup

```bash
# 1. Download RDS CA certificate
curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
mkdir -p certs
mv global-bundle.pem certs/rds-ca-bundle.pem

# 2. Use MySQL config
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/.env.rds-mysql
mv .env.rds-mysql .env

# 3. Edit with your MySQL RDS endpoint
nano .env

# RDS_DATABASE_URL format:
# mysql://nias_admin:YOUR_PASSWORD@anveesa-nias.xxxxx.us-east-1.rds.amazonaws.com:3306/nias?tls=custom&parseTime=true

# 4. Use MySQL docker-compose
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.rds-mysql.yml

# 5. Start
docker-compose -f docker-compose.rds-mysql.yml up -d
```

---

## 🧪 Test Connection

```bash
# PostgreSQL
psql -h anveesa-nias.xxxxx.us-east-1.rds.amazonaws.com -U nias_admin -d nias

# MySQL
mysql -h anveesa-nias.xxxxx.us-east-1.rds.amazonaws.com -u nias_admin -p -D nias
```

---

## 📋 Environment Variables Checklist

Required in `.env`:

- [ ] `RDS_DATABASE_URL` - Your RDS endpoint
- [ ] `JWT_SECRET` - Min 32 characters (generate with `openssl rand -hex 32`)
- [ ] `NIAS_ENCRYPTION_KEY` - Exactly 32 characters (generate with `openssl rand -hex 16`)
- [ ] `DEFAULT_ADMIN_PASSWORD` - Strong password for admin account

Optional but recommended:

- [ ] `DB_SSL_MODE=require` - Enable SSL/TLS
- [ ] `CORS_ORIGIN` - Your domain (e.g., `https://db.yourcompany.com`)
- [ ] `RATE_LIMIT_ENABLED=true` - Enable rate limiting

---

## ⚡ One-Liner Deploy

Once you have RDS created and `.env` configured:

```bash
# PostgreSQL RDS
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.rds-postgres.yml && \
docker-compose -f docker-compose.rds-postgres.yml up -d

# MySQL RDS
curl -O https://raw.githubusercontent.com/your-org/anveesa-nias/main/deploy/compose/docker-compose.rds-mysql.yml && \
docker-compose -f docker-compose.rds-mysql.yml up -d
```

---

## 🛠️ Troubleshooting

### Cannot connect to RDS

**Check 1:** Security group allows connection
```bash
# Test from your server
telnet anveesa-nias.xxxxx.rds.amazonaws.com 5432
```

**Check 2:** Verify credentials
```bash
# Test manual connection
psql -h anveesa-nias.xxxxx.rds.amazonaws.com -U nias_admin -d nias
```

**Check 3:** Check application logs
```bash
docker logs anveesa-nias
```

### SSL/TLS errors

For PostgreSQL:
```bash
# In .env, ensure:
DATABASE_URL=postgres://...?sslmode=require
DB_SSL_MODE=require
```

For MySQL:
```bash
# Ensure certificate is mounted:
# 1. Download: curl -O https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
# 2. Save to: ./certs/rds-ca-bundle.pem
# 3. Check docker-compose has: volumes: - ./certs:/app/certs:ro
```

---

## 💰 Cost Estimate

### Free Tier (12 months)
- **db.t2.micro** or **db.t3.micro**
- 20 GB storage
- **Cost**: $0 (first year)

### Small Production
- **db.t3.small** (2 vCPU, 2 GB RAM)
- 50 GB storage
- Single-AZ
- **Cost**: ~$30-40/month

### Medium Production
- **db.t3.medium** (2 vCPU, 4 GB RAM)
- 100 GB storage
- Multi-AZ (high availability)
- **Cost**: ~$100-150/month

### Large Production
- **db.m6g.large** (2 vCPU, 8 GB RAM)
- 500 GB storage
- Multi-AZ + Read Replicas
- **Cost**: $400-600/month

---

## 🎯 Production Checklist

Before going to production:

- [ ] Create RDS instance in correct region
- [ ] Enable Multi-AZ for high availability
- [ ] Enable automated backups (7-35 days)
- [ ] Enable storage encryption
- [ ] Enable SSL/TLS (sslmode=require)
- [ ] Use strong master password (20+ characters)
- [ ] Restrict security group to application only
- [ ] Place RDS in private subnet
- [ ] Set up CloudWatch alarms
- [ ] Enable Enhanced Monitoring
- [ ] Generate strong JWT_SECRET and NIAS_ENCRYPTION_KEY
- [ ] Set strong DEFAULT_ADMIN_PASSWORD
- [ ] Update CORS_ORIGIN to your domain
- [ ] Test backup/restore process
- [ ] Document connection details securely

---

## 📚 Full Documentation

For complete details, see:
- **[DEPLOY_RDS.md](./DEPLOY_RDS.md)** - Complete RDS deployment guide
- **[INSTALL.md](./INSTALL.md)** - All installation options
- **[README.md](./README.md)** - Project overview

---

## 🆘 Need Help?

1. Check application logs: `docker logs anveesa-nias`
2. Check RDS status in AWS Console
3. Test connection manually with `psql` or `mysql`
4. Verify security groups and network configuration
5. Ensure `.env` has all required variables

---

**🎉 You're all set!** Your application is now running on fully-managed AWS RDS.
