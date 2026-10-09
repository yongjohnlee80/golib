# parse/shell

Install: `go get github.com/yongjohnlee80/golib/parse/shell`.

`shell.Definition().NewSource(nil)` supplies POSIX/Bash lexical highlighting,
variables, quoting, heredoc delimiter queues, and keyword-block indentation.
Heredoc bodies do not supply structural indentation tokens. The default unit is
two spaces. Other shell dialects require their own tested provider.

Register the definition for shell files and `shell`/`sh`/`bash` fences. It does
not execute scripts or reformat source. See [languages](../languages/README.md)
and [LICENSE](../../LICENSE).
