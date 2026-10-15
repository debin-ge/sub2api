-- This fork already stores video and Windsurf platforms omitted by upstream 241.
-- Skip its CHECK-only migration before it can reject existing rows. The platform
-- registry is authoritative; 242 and final migration 281 remove both constraints.
-- Keep the upstream file immutable and pin the runner's TrimSpace SHA256.
INSERT INTO schema_migrations (filename, checksum)
VALUES ('241_add_typesafe_platform.sql', 'b6559525bf8d0b5d7c617e7415944f0d1fae8c408e9131ace84139795f66dcd2')
ON CONFLICT (filename) DO NOTHING;
