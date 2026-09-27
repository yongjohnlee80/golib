# yaml-test-suite, vendored

These test directories are the [yaml-test-suite](https://github.com/yaml/yaml-test-suite) as published at
the data tag `data-2022-01-17` (commit `5f49729`), **unmodified**. The `name/` and `tags/` index trees
are not included. `License` is copied from the source tag `v2022-01-17` (commit `6d48918`): the suite is
MIT-licensed, Copyright (c) 2016-2020 Ingy döt Net, and is distributed here under that licence, not
under golib's.

Each directory is one test (numbered subdirectories are subtests): `===` is its title, `in.yaml` its
input, `test.event` the expected event stream, `in.json` (when present) the expected JSON of each
document, and `error` (when present) marks an input that must fail to parse. `out.yaml` and
`emit.yaml` are an emitter's expected output; `parse/yaml` has no emitter and does not read them.

Updating the suite is a separate commit that changes only this directory.
