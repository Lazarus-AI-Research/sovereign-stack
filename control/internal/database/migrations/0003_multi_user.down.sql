DROP TABLE workspace_memberships;
DROP TABLE invitations;
DROP TABLE setup_claims;
ALTER TABLE admin_users
    DROP CONSTRAINT admin_users_role_check,
    DROP COLUMN disabled,
    DROP COLUMN role,
    DROP COLUMN display_name;
