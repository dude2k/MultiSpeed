ALTER TABLE settings ADD COLUMN ookla_eula_accepted INTEGER NOT NULL DEFAULT 0
    CHECK(ookla_eula_accepted IN (0, 1));

ALTER TABLE settings ADD COLUMN ookla_eula_accepted_at TEXT;
