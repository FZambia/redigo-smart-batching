# redigo-smart-batching

Some benchmarks to show how smart batching technique helps to maximize Redis connection throughput in Go (using pipelining).

Start Redis server and run:

```
go test -test.run=XXX -bench=.
```

## Results

Without pipelining

```
BenchmarkPublish01-4   	   21138	     53722 ns/op	      72 B/op	       3 allocs/op
BenchmarkPublish02-4   	   23191	     50568 ns/op	      32 B/op	       1 allocs/op
BenchmarkPublish03-4   	   56035	     19796 ns/op	      93 B/op	       3 allocs/op
BenchmarkPublish04-4   	   53565	     21203 ns/op	      62 B/op	       2 allocs/op
```

With pipelining:

```
BenchmarkPublish05-4   	  403075	      2950 ns/op	     481 B/op	       8 allocs/op
BenchmarkPublish06-4   	  389034	      2814 ns/op	     357 B/op	       6 allocs/op
BenchmarkPublish07-4   	  452078	      2554 ns/op	     149 B/op	       3 allocs/op
BenchmarkPublish08-4   	  396226	      2636 ns/op	      64 B/op	       3 allocs/op
```
