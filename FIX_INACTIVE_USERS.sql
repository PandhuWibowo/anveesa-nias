-- Fix for inactive users status
-- Run this to activate all existing users

-- Set all existing users to active
UPDATE users 
SET is_active = 1 
WHERE is_active IS NULL OR is_active = 0;

-- Verify
SELECT id, username, role, is_active, created_at 
FROM users 
ORDER BY id;
