-- sqlite
SELECT [bracket name], `tick`, "dq", x'0a1b', ?, ?1, :name, @name, $name FROM t;
CREATE TRIGGER tr AFTER INSERT ON t BEGIN
  UPDATE t SET n = n + 1;
END;
PRAGMA journal_mode = WAL;
SELECT * FROM t WHERE a GLOB 'x*' AND b REGEXP '.';
/* open block
