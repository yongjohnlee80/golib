# a hash comment; with a semicolon
SELECT `col`, `odd``name`, "a\"b", 'it\'s' FROM t WHERE id = ? AND balance--1 > 0;
-- a dash comment before a space
INSERT INTO t VALUES (1) ON DUPLICATE KEY UPDATE n = 2;
SELECT /*! STRAIGHT_JOIN */ a FROM b /*+ BKA(b) */;
CREATE TABLE t (id INT AUTO_INCREMENT, name VARCHAR(10), body LONGTEXT) ENGINE=InnoDB;
SELECT @var, @@session.x, 0x1f, 3.5, 1e9;
/* not /* nested */ SELECT 2;
SELECT "open
