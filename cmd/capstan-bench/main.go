// capstan-bench — measures provider API latency for the operations Marina
// needs to drive on each refresh. The data answers concrete questions:
//
//   1. Is subprocess-per-call to capstan acceptable for Marina v0.1, or do
//      we need a long-lived sidecar / daemon from day one?
//   2. How long after Create does the new server appear in List? (provider
//      eventual consistency)
//   3. How long does PowerOff → action complete take end-to-end?
//   4. Does fanout (parallel Get for N servers) improve list-detail latency
//      vs serial, or does the provider rate-limit it away?
//
// Read-only by default. Pass --include-mutations to also exercise
// Create / Power / Destroy, which costs real money (one server-hour) and
// requires write scope on the token.
//
// Usage:
//   HCLOUD_TOKEN=xxx capstan-bench --provider hetzner --iterations 30
//   HCLOUD_TOKEN=xxx capstan-bench --provider hetzner --include-mutations \
//                                  --server-type cax11 --region fsn1
//
// Output: Markdown summary to stdout. Use --raw <file> to also dump
// per-call NDJSON (one JSON object per measurement, for post-hoc analysis).

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/solcreek/capstan"
)

func main() {
	var (
		providerFlag = flag.String("provider", "hetzner", "provider name: hetzner|digitalocean|linode|vultr")
		iterations   = flag.Int("iterations", 30, "iterations per read operation")
		concurrency  = flag.String("concurrency", "1,5,10", "comma-separated concurrency levels for Get fanout")
		includeMut   = flag.Bool("include-mutations", false, "also bench Create/Power/Destroy (costs ~1 server-hour)")
		serverType   = flag.String("server-type", "cax11", "server type/plan for mutation test (defaults to cheapest Hetzner ARM)")
		region       = flag.String("region", "fsn1", "region for mutation test (defaults to Hetzner Falkenstein)")
		rawPath      = flag.String("raw", "", "if set, write per-call NDJSON to this file")
		timeoutSec   = flag.Int("timeout", 600, "overall bench timeout in seconds")
	)
	flag.Parse()

	pName := capstan.ProviderName(*providerFlag)
	token, envName := tokenForProvider(pName)
	if token == "" {
		fmt.Fprintf(os.Stderr, "capstan-bench: %s not set in env\n", envName)
		os.Exit(2)
	}

	p, err := capstan.New(pName, token)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capstan-bench: %v\n", err)
		os.Exit(2)
	}

	concLevels, err := parseConcurrency(*concurrency)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capstan-bench: %v\n", err)
		os.Exit(2)
	}

	var rawOut io.Writer
	if *rawPath != "" {
		f, err := os.Create(*rawPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "capstan-bench: open --raw: %v\n", err)
			os.Exit(2)
		}
		defer f.Close()
		rawOut = f
	}
	rec := newRecorder(rawOut)

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	fmt.Fprintf(os.Stderr, "capstan-bench: provider=%s iterations=%d concurrency=%v mutations=%v\n",
		pName, *iterations, concLevels, *includeMut)

	benchReads(ctx, p, *iterations, rec)
	benchFanout(ctx, p, concLevels, *iterations, rec)

	if *includeMut {
		benchMutations(ctx, p, *serverType, *region, rec)
	}

	rec.printMarkdown(os.Stdout, pName)
}

func tokenForProvider(p capstan.ProviderName) (token, envName string) {
	switch p {
	case capstan.Hetzner:
		return os.Getenv("HCLOUD_TOKEN"), "HCLOUD_TOKEN"
	case capstan.DigitalOcean:
		return os.Getenv("DIGITALOCEAN_TOKEN"), "DIGITALOCEAN_TOKEN"
	case capstan.Linode:
		return os.Getenv("LINODE_TOKEN"), "LINODE_TOKEN"
	case capstan.Vultr:
		return os.Getenv("VULTR_TOKEN"), "VULTR_TOKEN"
	default:
		return "", string(p)
	}
}

func parseConcurrency(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		n, err := strconv.Atoi(p)
		if err != nil || n <= 0 {
			return nil, fmt.Errorf("--concurrency: %q is not a positive integer", p)
		}
		out = append(out, n)
	}
	return out, nil
}

// recorder collects measurements and emits NDJSON + Markdown.

type recorder struct {
	mu   sync.Mutex
	out  io.Writer
	byOp map[string]*stats
	enc  *json.Encoder
}

func newRecorder(rawOut io.Writer) *recorder {
	r := &recorder{out: rawOut, byOp: map[string]*stats{}}
	if rawOut != nil {
		r.enc = json.NewEncoder(rawOut)
	}
	return r
}

type measurement struct {
	Op        string  `json:"op"`
	Iter      int     `json:"iter"`
	LatencyMs float64 `json:"latencyMs"`
	OK        bool    `json:"ok"`
	Error     string  `json:"error,omitempty"`
	Ts        string  `json:"ts"`
}

func (r *recorder) record(op string, iter int, dur time.Duration, err error) {
	ms := float64(dur.Microseconds()) / 1000.0

	r.mu.Lock()
	s, ok := r.byOp[op]
	if !ok {
		s = &stats{name: op}
		r.byOp[op] = s
	}
	s.add(ms, err == nil)
	r.mu.Unlock()

	if r.enc != nil {
		m := measurement{
			Op: op, Iter: iter, LatencyMs: ms, OK: err == nil,
			Ts: time.Now().UTC().Format(time.RFC3339Nano),
		}
		if err != nil {
			m.Error = err.Error()
		}
		r.mu.Lock()
		r.enc.Encode(m)
		r.mu.Unlock()
	}
}

func (r *recorder) printMarkdown(w io.Writer, p capstan.ProviderName) {
	// Stable order: read ops first, then fanout, then mutations.
	ops := make([]string, 0, len(r.byOp))
	for k := range r.byOp {
		ops = append(ops, k)
	}
	sort.Strings(ops)

	fmt.Fprintf(w, "# capstan-bench — %s\n\n", p)
	fmt.Fprintf(w, "Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))
	fmt.Fprintf(w, "| Operation | n | err | p50 (ms) | p95 (ms) | p99 (ms) | min | max | mean |\n")
	fmt.Fprintf(w, "|---|---:|---:|---:|---:|---:|---:|---:|---:|\n")
	for _, op := range ops {
		s := r.byOp[op].summary()
		fmt.Fprintf(w, "| `%s` | %d | %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f |\n",
			op, s.N, s.Errs, s.P50, s.P95, s.P99, s.Min, s.Max, s.Mean)
	}
	fmt.Fprintln(w)
}

// --- Read suite ---

func benchReads(ctx context.Context, p capstan.Provider, iters int, rec *recorder) {
	timed(ctx, "Regions", iters, rec, func(ctx context.Context) error {
		_, err := p.Regions(ctx)
		return err
	})
	timed(ctx, "Plans", iters, rec, func(ctx context.Context) error {
		_, err := p.Plans(ctx, "")
		return err
	})
	timed(ctx, "List", iters, rec, func(ctx context.Context) error {
		_, err := p.List(ctx, capstan.ListOpts{MaxServers: 200})
		return err
	})

	// Get requires an existing server. Reuse one from List if any.
	servers, err := p.List(ctx, capstan.ListOpts{MaxServers: 1})
	if err != nil || len(servers) == 0 {
		fmt.Fprintf(os.Stderr, "capstan-bench: skipping Get (no existing servers or list failed: %v)\n", err)
		return
	}
	id := servers[0].ID
	timed(ctx, "Get", iters, rec, func(ctx context.Context) error {
		_, err := p.Get(ctx, id)
		return err
	})
}

func timed(ctx context.Context, op string, iters int, rec *recorder, fn func(context.Context) error) {
	for i := 0; i < iters; i++ {
		if ctx.Err() != nil {
			return
		}
		t := time.Now()
		err := fn(ctx)
		rec.record(op, i, time.Since(t), err)
	}
}

// --- Fanout (parallel Get) suite ---

func benchFanout(ctx context.Context, p capstan.Provider, levels []int, iters int, rec *recorder) {
	servers, err := p.List(ctx, capstan.ListOpts{MaxServers: 1})
	if err != nil || len(servers) == 0 {
		return
	}
	id := servers[0].ID

	for _, n := range levels {
		op := fmt.Sprintf("Get_fanout_x%d", n)
		for i := 0; i < iters; i++ {
			if ctx.Err() != nil {
				return
			}
			t := time.Now()
			var wg sync.WaitGroup
			errs := make(chan error, n)
			for j := 0; j < n; j++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := p.Get(ctx, id)
					if err != nil {
						errs <- err
					}
				}()
			}
			wg.Wait()
			close(errs)
			var firstErr error
			for e := range errs {
				if firstErr == nil {
					firstErr = e
				}
			}
			rec.record(op, i, time.Since(t), firstErr)
		}
	}
}

// --- Mutation suite (--include-mutations) ---

func benchMutations(ctx context.Context, p capstan.Provider, plan, region string, rec *recorder) {
	name := fmt.Sprintf("capstan-bench-%d", time.Now().Unix())
	fmt.Fprintf(os.Stderr, "capstan-bench: creating mutation-test server %q (%s @ %s)\n", name, plan, region)

	t0 := time.Now()
	server, err := p.Create(ctx, capstan.CreateOpts{
		Name:   name,
		Plan:   plan,
		Region: region,
	})
	rec.record("Create_ack", 0, time.Since(t0), err)
	if err != nil {
		fmt.Fprintf(os.Stderr, "capstan-bench: Create failed: %v\n", err)
		return
	}

	// Always destroy at the end, even on subsequent failure.
	defer func() {
		fmt.Fprintf(os.Stderr, "capstan-bench: destroying %s\n", server.ID)
		td := time.Now()
		derr := p.Destroy(context.Background(), server.ID)
		rec.record("Destroy_ack", 0, time.Since(td), derr)
		if derr != nil {
			fmt.Fprintf(os.Stderr, "capstan-bench: destroy failed: %v — clean up manually\n", derr)
		}
	}()

	// Eventual visibility: how long after Create until the server appears in List?
	tv := time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		servers, lerr := p.List(ctx, capstan.ListOpts{MaxServers: 200})
		if lerr == nil && containsID(servers, server.ID) {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	rec.record("Create_visible_in_list", 0, time.Since(tv), nil)

	// Wait for status=running (provider eventual consistency on power).
	tr := time.Now()
	for {
		if ctx.Err() != nil {
			return
		}
		s, gerr := p.Get(ctx, server.ID)
		if gerr == nil && s.Status == capstan.StatusRunning {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	rec.record("Create_running", 0, time.Since(tr), nil)

	// PowerOff → WaitForAction
	t1 := time.Now()
	action, err := p.PowerOff(ctx, server.ID)
	rec.record("PowerOff_ack", 0, time.Since(t1), err)
	if err == nil {
		t2 := time.Now()
		_, werr := p.WaitForAction(ctx, action.ID)
		rec.record("PowerOff_complete", 0, time.Since(t2), werr)
	}

	// PowerOn → WaitForAction
	t3 := time.Now()
	action, err = p.PowerOn(ctx, server.ID)
	rec.record("PowerOn_ack", 0, time.Since(t3), err)
	if err == nil {
		t4 := time.Now()
		_, werr := p.WaitForAction(ctx, action.ID)
		rec.record("PowerOn_complete", 0, time.Since(t4), werr)
	}

	// Restart → WaitForAction
	t5 := time.Now()
	action, err = p.Restart(ctx, server.ID)
	rec.record("Restart_ack", 0, time.Since(t5), err)
	if err == nil {
		t6 := time.Now()
		_, werr := p.WaitForAction(ctx, action.ID)
		rec.record("Restart_complete", 0, time.Since(t6), werr)
	}
}

func containsID(servers []capstan.Server, id string) bool {
	for _, s := range servers {
		if s.ID == id {
			return true
		}
	}
	return false
}
