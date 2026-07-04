// Sentinel module: excludes ui/ (and stray .go files inside node_modules,
// e.g. flatted's Go port) from the parent module's ./... package set so
// build/vet/test/revive never touch frontend dependency trees.
module github.com/c360studio/semboids/ui-exclude

go 1.26.3
