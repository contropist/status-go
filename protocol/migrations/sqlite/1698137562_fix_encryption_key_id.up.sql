CREATE TABLE hash_ratchet_encryption_v2 (
  group_id BLOB NOT NULL,
  deprecated_key_id INT NOT NULL,
  key BLOB NOT NULL,
  key_timestamp BLOB,
  key_id BLOB NOT NULL,
  PRIMARY KEY(key_id) ON CONFLICT REPLACE
);

INSERT INTO hash_ratchet_encryption_v2(group_id, deprecated_key_id, key, key_id) SELECT group_id, key_id, key, group_id || key_id FROM hash_ratchet_encryption;

DROP TABLE hash_ratchet_encryption_cache;

DROP TABLE hash_ratchet_encryption;

ALTER TABLE hash_ratchet_encryption_v2 RENAME TO hash_ratchet_encryption;

UPDATE hash_ratchet_encryption SET key_timestamp = deprecated_key_id;

CREATE TABLE hash_ratchet_encryption_cache (
  group_id BLOB NOT NULL,
  key_id int NOT NULL,
  seq_no INTEGER,
  hash BLOB NOT NULL,
  sym_enc_key BLOB
);
