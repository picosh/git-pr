# dev script

`go run ./contrib/dev/`

If you want to instead use this to bootstrap:

1. `go run ./contrib/dev/ --cleanup=false`
2. Note the tmp dir printed out
3. Stop the program `Ctrl+C`
4. Modify as needed within the tmp dir
5. Run `git-dir` and point at the config contained within the tmp dir
6. Remember to clean up the tmp dir yourself when finished
