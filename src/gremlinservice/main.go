package main

import (
	"flag"
	"fmt"
	"go.uber.org/zap"
	"path/filepath"
	"strconv"
	"time"
)

var (
	jaegerAddr string
	faultAddr  string

	zLogger *zap.Logger
	sugar   *zap.SugaredLogger
)

func init() {
	flag.StringVar(&jaegerAddr, "jaeger", "cs1380.cs.brown.edu:5000", "address of jaeger service")
	flag.StringVar(&faultAddr, "fault", "cs1380.cs.brown.edu:5000", "address of fault service")

	zLogger, _ = zap.NewProduction()
	sugar = zLogger.Sugar()
}

func main() {
	flag.Parse()

	defer zLogger.Sync()

	// Create jaeger service client
	jc := NewJaegerClient(jaegerAddr)

	// Create fault service client
	fc := NewFaultServiceClient(faultAddr)

	// Create experiment id (just use unix timestamp for now)
	id := strconv.FormatInt(time.Now().Unix(), 10)

	// 1. Based on csv data, choose services that will have fault injection.
	// 	  For now, only most frequent service will be fault injected
	records, err := readCSV(filepath.Join("csv", "services"))
	if err != nil {
		sugar.Fatal(err)
	}

	row := records[0]
	faultSvc := row["service"]

	// 2. Find upstream services for to-be fault injected services. This includes
	// 	  all upstream services of those who are immediately upstream of to-be fault
	//	  injected service, and so on.
	records, err = readCSV(filepath.Join("csv", "edges"))
	if err != nil {
		sugar.Fatal(err)
	}

	// Create reverse graph of microservice mesh
	mesh := make(map[string]map[string]struct{}, 0)
	for _, row := range records {
		start := row["start"]
		end := row["end"]

		if _, ok := mesh[end]; !ok {
			mesh[end] = make(map[string]struct{}, 0)
		}

		mesh[end][start] = struct{}{}
	}

	// Get all upstream services of to-be fault injected service using dfs
	upstreamSvcsMap := make(map[string]struct{}, 0)

	var stack []string
	for node := range mesh[faultSvc] {
		stack = append(stack, node)
	}

	var node string
	for len(stack) > 0 {
		node, stack = stack[0], stack[1:]
		upstreamSvcsMap[node] = struct{}{}
		for n := range mesh[node] {
			stack = append(stack, n)
		}
	}

	var upstreamSvcs []string
	for svc := range upstreamSvcsMap {
		upstreamSvcs = append(upstreamSvcs, svc)
	}

	// 3. Get traces for upstream services before fault injection for last 30 seconds
	chunks, err := jc.QueryChunks(id, Before, upstreamSvcs, time.Now().Add(-30*time.Second))
	if err != nil {
		sugar.Fatal(err)
	}

	// 4. Measure stats for upstream services' traces
	beforeNodes, err := MeasureSuccessRate(chunks)
	if err != nil {
		sugar.Fatal(err)
	}
	sugar.Info("Stats before fault injection:")
	fmt.Printf("%#v", beforeNodes)

	// 5. Apply fault injection
	sugar.Info("Applying fault injection...")
	if err := fc.ApplyFault(faultSvc); err != nil {
		sugar.Fatal(err)
	}

	// 6. Wait 30 seconds
	sugar.Info("Waiting 30 seconds for experiment to run...")
	time.Sleep(30 * time.Second)

	// 7. Measure traces for upstream services after fault injection for last 30 seconds
	chunks, err = jc.QueryChunks(id, After, upstreamSvcs, time.Now().Add(-30*time.Second))
	if err != nil {
		sugar.Fatal(err)
	}

	// 8. Delete fault injection
	sugar.Info("Deleting fault injection...")
	if err := fc.DeleteFault(faultSvc); err != nil {
		sugar.Fatal(err)
	}

	// 9. Analyze results
	afterNodes, err := MeasureSuccessRate(chunks)
	if err != nil {
		sugar.Fatal(err)
	}
	sugar.Info("Stats after fault injection:")
	fmt.Printf("%#v", afterNodes)
}
