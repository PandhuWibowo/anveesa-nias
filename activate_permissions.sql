-- Activate Permission System for Admin
-- This script ensures the admin role has all necessary permissions

-- 1. Update admin role with all permissions
UPDATE roles 
SET permissions = '["view_connections","create_connections","delete_connections","view_users","manage_users","view_roles","manage_roles","view_folders","manage_folders","select","insert","update","delete","create","alter","drop"]'
WHERE name = 'admin';

-- 2. Update user role with basic permissions
UPDATE roles 
SET permissions = '["view_connections","view_folders","select"]'
WHERE name = 'user';

-- 3. Set all existing connections to have an owner (assign to first admin)
UPDATE connections 
SET owner_id = (SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1)
WHERE owner_id IS NULL;

-- 4. Set all users to active
UPDATE users 
SET is_active = 1 
WHERE is_active IS NULL OR is_active = 0;

-- 5. Verify admin role permissions
SELECT id, name, permissions, is_system 
FROM roles 
WHERE name IN ('admin', 'user');

-- 6. Verify users
SELECT id, username, role, role_id, is_active 
FROM users 
ORDER BY id;

-- 7. Verify connections have owners
SELECT id, name, driver, owner_id 
FROM connections 
ORDER BY id;
