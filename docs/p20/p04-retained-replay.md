# P04 evidence retention recovery

The original P04 source remains `659e25e3c5e263ffb3dd74cde953e812b75a7439`.
Its original run `32392744860`, artifact `9415518410`, and recorded digest remain
historical authority. The artifact expired; GitHub rejected rerunning the original
run because it was more than one month old. No historical review is re-signed.

The dedicated historical replay workflow checked out that exact source and ran
all ten P04 cases. Run `35754013183`, workflow head
`281519b3ede0aeba179e3e069772a184bf54bcff`, produced artifact `10708499392`:

`sha256:a7f219b2f3a3f71dbd0a9d620d70a7a99f56083f0a8bd8c6ec3d0d9d02501143`

P18 now preserves the original metadata and adds a `retained_replay` record.
The binder verifies live successful run/retention metadata, the archive digest,
its original source and evidence-index SHA, every indexed file hash, and all ten
individual PASS results. Archive verification failure or future expiration stops
the binder. A newer candidate or arbitrary successful run cannot replace this pin.
The retained replay does not authorize a merge or claim current P20 completion.
