# Changelog

Use spec: https://common-changelog.org/

## v2026-02-25

### Added

- Ability to delete repo `ssh pr.pico.sh repo rm {name}`, must provide `--write` to persist

### Changed

- Replaced `--comment` flag which was a string into a bool and now require comment to be provided by stdin for commands `accept`, `close`, and `reopen`
  - `echo "lgtm!" | ssh pr.pico.sh pr accept --comment 100`
  - If no `--comment` flag provided then you don't need to provide stdin

### Fixed

- Mangled formatting for `ssh pr.pico.sh` help text

## v2026-02-24

### Changed

- Added `ssh {username}@pr register` command and now require explicit registration to use this service
- Upgraded to `go1.25`
- Removed charm's `wish` with pico's `pssh`
