ALTER TABLE results ADD COLUMN tls_verification_disabled INTEGER NOT NULL DEFAULT 0
    CHECK(tls_verification_disabled IN (0, 1));
