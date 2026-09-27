# vfs/vfstest

The conformance suite for `vfs.FS` drivers.

```go
func TestConformance(t *testing.T) {
	vfstest.TestFS(t, func(t *testing.T) vfs.FS {
		f, err := mydriver.New(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		return f
	})
}
```

Each cell gets a fresh, empty filesystem. The core cells run for every driver: write/read/stat,
versions, offsets, invalid names across every verb, not-exist, directories, remove, rename, walk and
close. Each capability has its own cell group — `ConditionalWriter/*`, `ExclusiveCreator`,
`NoReplaceRenamer`, `Watcher`, `Copier` — run when the driver implements it and skipped, by name, when
it does not. A capability the platform refuses at runtime skips its cell with that error, so a refusal
is visible in the test output rather than passing silently.
