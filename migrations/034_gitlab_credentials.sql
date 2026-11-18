-- Personal GitLab site and access token. Apply manually; startup does not
-- run this script. gorm AutoMigrate also adds the columns on process start.

ALTER TABLE user_configs ADD COLUMN gitlab_base_url TEXT NULL;
ALTER TABLE user_configs ADD COLUMN gitlab_token TEXT NULL;
