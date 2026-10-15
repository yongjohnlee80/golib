-- TODO: index this; FIXME later
SELECT id, count(*), now () FROM users u WHERE u.id = $1 AND name <> 'it''s';
select x::int, 3.5, 1e9, 0x1f, 42 from t limit 42;
SELECT E'a\nb\'c', 'a\nb', B'1010', X'1f';
/* a block comment
   that spans /* nested */ lines */ SELECT 1;
CREATE FUNCTION f() RETURNS int AS $body$
BEGIN
  RETURN 1; -- inside the body
END;
$body$ LANGUAGE plpgsql;
SELECT $$one; two$$, $tag$x$tag$, "Quoted ""Ident""", @name, :name;
CASE WHEN a THEN 1 ELSE 2 END;
CREATE TABLE t (id BIGINT PRIMARY KEY, doc JSONB, at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP);
INSERT INTO t VALUES (1, '{"a": 1}', NULL) ON CONFLICT DO NOTHING RETURNING *;
SELECT 'unterminated
