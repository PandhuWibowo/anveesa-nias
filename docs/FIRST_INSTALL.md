# First Installation Guide

## 🚀 Quick Start with Docker

### 1. Prepare Environment

```bash
# Clone repository
git clone <repository-url>
cd anveesa-nias

# Copy and edit environment file
cp .env.example .env
nano .env  # or vim, code, etc.
```

### 2. **IMPORTANT**: Set Secure Credentials

Edit `.env` and change these values:

```env
# Generate secure secrets
JWT_SECRET=$(openssl rand -hex 32)
NIAS_ENCRYPTION_KEY=$(openssl rand -hex 16)

# Set your admin credentials
DEFAULT_ADMIN_USERNAME=admin
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!

# Set your domain (if using reverse proxy)
CORS_ORIGIN=https://yourdomain.com
```

### 3. Start the Application

```bash
docker-compose up -d
```

### 4. Get Your Admin Credentials

View the startup logs to see your admin account:

```bash
docker-compose logs nias | grep -A 4 "Default admin"
```

**Expected output:**
```
✓ Default admin account created: admin
  Username: admin
  Password: YourSecurePassword123!
  Please change the password after first login!
```

### 5. First Login

1. Open your browser: `http://localhost:8080`
2. Login with the admin credentials from step 4
3. **Change your password immediately** in the user settings

## 🔐 Security Checklist

- [ ] Changed `JWT_SECRET` from default
- [ ] Changed `NIAS_ENCRYPTION_KEY` from default  
- [ ] Set secure `DEFAULT_ADMIN_PASSWORD` (not `Admin123!`)
- [ ] Changed admin password after first login
- [ ] Configured `CORS_ORIGIN` to your actual domain
- [ ] Using HTTPS with reverse proxy (production)
- [ ] Database volume is backed up regularly

## ⚠️ Production Requirements

**The application WILL NOT START in production mode with default passwords!**

You must set these environment variables:

```env
NIAS_ENV=production
JWT_SECRET=<min-32-characters>
NIAS_ENCRYPTION_KEY=<exactly-32-characters>
DEFAULT_ADMIN_PASSWORD=<not-Admin123!>
```

## 🐛 Troubleshooting

### "Cannot login - invalid credentials"

```bash
# Check if admin was created
docker-compose logs nias | grep "Default admin"
```

### "Database is locked"

```bash
# Restart the container
docker-compose restart nias
```

### "FATAL: DEFAULT_ADMIN_PASSWORD must be set in production"

**Solution:** Set a secure password in your `.env` file:

```env
DEFAULT_ADMIN_PASSWORD=YourSecurePassword123!
```

### Reset Everything (⚠️ Deletes all data)

```bash
docker-compose down -v
docker-compose up -d
```

## 📝 What Happens on First Start?

1. **Database Initialization**
   - SQLite database created at `/app/data/nias.db`
   - All tables and indexes created
   - System roles (admin, user) seeded

2. **Default Admin Creation**
   - Checks if any users exist
   - If none exist, creates admin account
   - Password is hashed with bcrypt (cost 12)
   - Credentials printed to console logs

3. **Ready for Use**
   - Application listening on port 8080
   - Health check available at `/health`
   - Ready to accept connections

## 🔄 Subsequent Starts

On subsequent starts (when users already exist):

- Default admin account is **NOT** recreated
- Existing users and connections remain unchanged
- Only database migrations are applied (if any)

## 📚 Next Steps

After successful installation:

1. ✅ Login with admin account
2. ✅ Change admin password
3. ✅ Create database connections
4. ✅ Invite team members (create user accounts)
5. ✅ Set up roles and permissions
6. ✅ Configure backup strategy

## 📖 Additional Documentation

- [DOCKER.md](./DOCKER.md) - Detailed Docker deployment guide
- [README.md](./README.md) - General documentation and API reference
- `.env.example` - All available environment variables

## 🆘 Need Help?

- Check logs: `docker-compose logs -f nias`
- Health check: `curl http://localhost:8080/health`
- Version info: `curl http://localhost:8080/version`
- GitHub Issues: [repository-url/issues]
