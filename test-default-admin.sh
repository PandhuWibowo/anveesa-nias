#!/bin/bash
# Test script for default admin account creation

set -e

echo "🧪 Testing Default Admin Account Feature"
echo "=========================================="
echo ""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test 1: Check if code compiles
echo "📦 Test 1: Checking if Go code compiles..."
cd server
if go build -o /tmp/nias-test .; then
    echo -e "${GREEN}✓${NC} Code compiles successfully"
    rm -f /tmp/nias-test
else
    echo -e "${RED}✗${NC} Code compilation failed"
    exit 1
fi
cd ..
echo ""

# Test 2: Check Docker Compose configuration
echo "🐳 Test 2: Validating Docker Compose configuration..."
if docker-compose config > /dev/null 2>&1; then
    echo -e "${GREEN}✓${NC} Docker Compose configuration is valid"
else
    echo -e "${RED}✗${NC} Docker Compose configuration is invalid"
    exit 1
fi
echo ""

# Test 3: Check required environment variables in docker-compose.yml
echo "🔧 Test 3: Checking environment variables..."
if docker-compose config | grep -q "DEFAULT_ADMIN_USERNAME"; then
    echo -e "${GREEN}✓${NC} DEFAULT_ADMIN_USERNAME is configured"
else
    echo -e "${RED}✗${NC} DEFAULT_ADMIN_USERNAME is missing"
    exit 1
fi

if docker-compose config | grep -q "DEFAULT_ADMIN_PASSWORD"; then
    echo -e "${GREEN}✓${NC} DEFAULT_ADMIN_PASSWORD is configured"
else
    echo -e "${RED}✗${NC} DEFAULT_ADMIN_PASSWORD is missing"
    exit 1
fi
echo ""

# Test 4: Check if .env.example has new variables
echo "📝 Test 4: Checking .env.example..."
if grep -q "DEFAULT_ADMIN_USERNAME" .env.example; then
    echo -e "${GREEN}✓${NC} DEFAULT_ADMIN_USERNAME documented in .env.example"
else
    echo -e "${YELLOW}⚠${NC} DEFAULT_ADMIN_USERNAME not found in .env.example"
fi

if grep -q "DEFAULT_ADMIN_PASSWORD" .env.example; then
    echo -e "${GREEN}✓${NC} DEFAULT_ADMIN_PASSWORD documented in .env.example"
else
    echo -e "${YELLOW}⚠${NC} DEFAULT_ADMIN_PASSWORD not found in .env.example"
fi
echo ""

# Test 5: Check documentation
echo "📚 Test 5: Checking documentation..."
if [ -f "DOCKER.md" ]; then
    echo -e "${GREEN}✓${NC} DOCKER.md exists"
else
    echo -e "${YELLOW}⚠${NC} DOCKER.md not found"
fi

if [ -f "FIRST_INSTALL.md" ]; then
    echo -e "${GREEN}✓${NC} FIRST_INSTALL.md exists"
else
    echo -e "${YELLOW}⚠${NC} FIRST_INSTALL.md not found"
fi

if grep -q "Default Admin Account" README.md; then
    echo -e "${GREEN}✓${NC} README.md documents default admin account"
else
    echo -e "${YELLOW}⚠${NC} Default admin not documented in README.md"
fi
echo ""

# Test 6: Check for required imports in db.go
echo "🔍 Test 6: Checking db.go implementation..."
if grep -q "golang.org/x/crypto/bcrypt" server/db/db.go; then
    echo -e "${GREEN}✓${NC} bcrypt import present"
else
    echo -e "${RED}✗${NC} bcrypt import missing"
    exit 1
fi

if grep -q "seedDefaultAdmin" server/db/db.go; then
    echo -e "${GREEN}✓${NC} seedDefaultAdmin function present"
else
    echo -e "${RED}✗${NC} seedDefaultAdmin function missing"
    exit 1
fi

if grep -q "hashPassword" server/db/db.go; then
    echo -e "${GREEN}✓${NC} hashPassword function present"
else
    echo -e "${RED}✗${NC} hashPassword function missing"
    exit 1
fi
echo ""

echo "=========================================="
echo -e "${GREEN}✓ All tests passed!${NC}"
echo ""
echo "📋 Next Steps:"
echo "1. Set secure credentials in .env file"
echo "2. Run: docker-compose up -d"
echo "3. Check logs: docker-compose logs -f nias"
echo "4. Look for: '✓ Default admin account created'"
echo "5. Login with the credentials from logs"
echo ""
echo "📖 Documentation:"
echo "- Quick Start: FIRST_INSTALL.md"
echo "- Docker Guide: DOCKER.md"
echo "- Full Changelog: CHANGELOG_DEFAULT_ADMIN.md"
