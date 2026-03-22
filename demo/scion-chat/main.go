package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"

	"github.com/scionproto/scion/pkg/addr"
	"github.com/scionproto/scion/pkg/daemon"
	"github.com/scionproto/scion/pkg/snet"
	"github.com/scionproto/scion/pkg/snet/addrutil"
	"github.com/scionproto/scion/private/app/path"
)

// Simple chat over SCION/UDP. Run one side with -mode server and the other with
// -mode client. The client resolves a path via the daemon, optionally using a
// private ISD membership.
func main() {
	var (
		mode        = flag.String("mode", "server", "server|client")
		daemonAddr  = flag.String("daemon", "", "SCION daemon address")
		dstIAFlag   = flag.String("dst-ia", "", "destination IA (client)")
		dstIPFlag   = flag.String("dst-ip", "", "destination IP (client)")
		dstPort     = flag.Int("dst-port", 4242, "destination port (client)")
		listenPort  = flag.Int("listen-port", 4242, "listen port (server)")
		privateISD  = flag.Uint("isd", 0, "private ISD membership (0=public)")
		privateOnly = flag.Bool("private-only", false, "restrict to private paths")
		refresh     = flag.Bool("refresh", true, "request fresh paths from daemon")
	)
	flag.Parse()

	ctx := context.Background()
	addrStr := *daemonAddr
	if addrStr == "" {
		fmt.Fprintln(os.Stderr, "daemon address required: set -daemon or SCION_DAEMON_ADDRESS")
		os.Exit(1)
	}
	sd, err := daemon.NewService(addrStr).Connect(ctx)
	check(err)
	defer sd.Close()
	topo, err := daemon.LoadTopology(ctx, sd)
	check(err)

	switch *mode {
	case "server":
		runServer(ctx, sd, topo, *listenPort)
	case "client":
		if *dstIAFlag == "" || *dstIPFlag == "" {
			fmt.Println("need -dst-ia and -dst-ip")
			os.Exit(1)
		}
		dstIA, err := addr.ParseIA(*dstIAFlag)
		check(err)
		runClient(ctx, sd, topo, dstIA, net.ParseIP(*dstIPFlag), *dstPort,
			addr.ISD(*privateISD), *privateOnly, *refresh)
	default:
		fmt.Println("mode must be server or client")
		os.Exit(1)
	}
}

func runServer(ctx context.Context, sd daemon.Connector, topo snet.Topology, port int) {
	localIP, err := addrutil.DefaultLocalIP(ctx, daemon.TopoQuerier{Connector: sd})
	check(err)
	sn := &snet.SCIONNetwork{Topology: topo}
	conn, err := sn.Listen(ctx, "udp", &net.UDPAddr{IP: localIP, Port: port})
	check(err)
	defer conn.Close()
	fmt.Printf("server listening on %s IA %s\n", conn.LocalAddr(), topo.LocalIA)

	var (
		lastAddr net.Addr
		mu       sync.Mutex
	)

	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			mu.Lock()
			dst := lastAddr
			mu.Unlock()
			if dst == nil {
				continue
			}
			msg := scanner.Bytes()
			if _, err := conn.WriteTo(msg, dst); err != nil {
				fmt.Println("send error:", err)
			}
		}
		if err := scanner.Err(); err != nil {
			fmt.Println("stdin error:", err)
		}
	}()

	buf := make([]byte, 2048)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			fmt.Println("read error:", err)
			return
		}
		mu.Lock()
		lastAddr = from
		mu.Unlock()
		msg := strings.TrimSpace(string(buf[:n]))
		fmt.Printf("recv from %v: %s\n", from, msg)
	}
}

func runClient(ctx context.Context, sd daemon.Connector, topo snet.Topology,
	dstIA addr.IA, dstIP net.IP, dstPort int, isd addr.ISD, privOnly, refresh bool) {

	localIP, err := addrutil.DefaultLocalIP(ctx, daemon.TopoQuerier{Connector: sd})
	check(err)
	opts := []path.Option{
		path.WithPrivateOnly(privOnly),
		path.WithRefresh(refresh),
	}
	if isd != 0 {
		srcIA, err := addr.IAFrom(isd, topo.LocalIA.AS())
		check(err)
		opts = append(opts, path.WithSourceIA(srcIA))
	}
	p, err := path.Choose(ctx, sd, dstIA, opts...)
	check(err)
	nextHop := p.UnderlayNextHop()

	// Ensure the SCION header source IA matches the membership used for path lookup.
	dialTopo := topo
	if src := p.Source(); src != 0 {
		dialTopo.LocalIA = src
	} else {
		ifaces := p.Metadata().Interfaces
		dialTopo.LocalIA = ifaces[0].IA
	}
	sn := &snet.SCIONNetwork{Topology: dialTopo}
	conn, err := sn.Dial(ctx, "udp", &net.UDPAddr{IP: localIP}, &snet.UDPAddr{
		IA:      dstIA,
		Path:    p.Dataplane(),
		NextHop: nextHop,
		Host:    &net.UDPAddr{IP: dstIP, Port: dstPort},
	})
	check(err)
	defer conn.Close()
	fmt.Printf("client connected to %s via %s\n", dstIA, p)

	recvDone := make(chan struct{})
	go func() {
		defer close(recvDone)
		buf := make([]byte, 2048)
		for {
			n, from, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			fmt.Printf("recv from %v: %s\n", from, strings.TrimSpace(string(buf[:n])))
		}
	}()

	sc := bufio.NewScanner(os.Stdin)
	fmt.Println("type messages, Ctrl+D to quit")
	for sc.Scan() {
		msg := sc.Bytes()
		if _, err := conn.Write(msg); err != nil {
			fmt.Println("write error:", err)
			break
		}
	}
	_ = conn.Close()
	<-recvDone
	check(sc.Err())
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
