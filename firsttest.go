package conntrack_accounting

import (
	"fmt"
	"github.com/mdlayher/netlink"
	"github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
	"log"
	"net"
	"os/exec"
	"time"
)

// echo 1 > /proc/sys/net/netfilter/nf_conntrack_acct
// echo 1 > /proc/sys/net/netfilter/nf_conntrack_timestamp

const PROTO_TCP = 6

const (
	TCP_CONNTRACK_NONE        = 0
	TCP_CONNTRACK_SYN_SENT    = 1
	TCP_CONNTRACK_SYN_RECV    = 2
	TCP_CONNTRACK_ESTABLISHED = 3
	TCP_CONNTRACK_FIN_WAIT    = 4
	TCP_CONNTRACK_CLOSE_WAIT  = 5
	TCP_CONNTRACK_LAST_ACK    = 6
	TCP_CONNTRACK_TIME_WAIT   = 7
	TCP_CONNTRACK_CLOSE       = 8
	TCP_CONNTRACK_LISTEN      = 9
	TCP_CONNTRACK_MAX         = 10
	TCP_CONNTRACK_IGNORE      = 11
	TCP_CONNTRACK_RETRANS     = 12
	TCP_CONNTRACK_UNACK       = 13
	TCP_CONNTRACK_TIMEOUT_MAX = 14
)

var subnetFilterAddr = net.IPv4(10, 32, 0, 0)
var subnetFilterMask = net.IPMask{255, 255, 0, 0}

var connections = make(map[uint32]struct{})


func dumpTable() {
	conn, err := conntrack.Dial(nil)
	if err != nil {
		log.Fatal(err)
	}
	df, err := conn.DumpFilter(conntrack.Filter{Mark: 0, Mask: 0})
	if err != nil {
		log.Fatal(err)
	}
	for _, flow := range df {
		if _, exists := connections[flow.ID]; exists {
			fmt.Println("- ", flow)
		}
	}
}


func main() {
	fmt.Println("Hello World!")

	conn, err := conntrack.Dial(nil)
	if err != nil {
		log.Fatal(err)
	}

	eventChannel := make(chan conntrack.Event, 1024)
	errorChannel, err := conn.Listen(eventChannel, 4, append(netfilter.GroupsCT, netfilter.GroupAcctQuota))
	if err != nil {
		log.Fatal(err)
	}

	err = conn.SetOption(netlink.ListenAllNSID, true)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		start := time.Now()

		for {
			event := <-eventChannel
			if event.Flow == nil || event.Flow.TupleOrig.IP.IsIPv6() || event.Flow.TupleOrig.Proto.Protocol != PROTO_TCP {
				continue
			}
			if !event.Flow.TupleOrig.IP.DestinationAddress.Mask(subnetFilterMask).Equal(subnetFilterAddr) {
				continue
			}
			if event.Flow.TupleOrig.Proto.DestinationPort != 31337 && event.Flow.TupleOrig.Proto.DestinationPort != 31338 {
				continue
			}

			switch event.Type {
			case conntrack.EventNew:
				fmt.Println(time.Now().Sub(start), "NEW Event:     ", event.Flow.ID, event, )
				connections[event.Flow.ID] = struct{}{}
			case conntrack.EventDestroy:
				fmt.Println(time.Now().Sub(start), "DESTROY Event: ", event.Flow.ID, event)
				delete(connections, event.Flow.ID)
			case conntrack.EventUpdate:
				if _, exists := connections[event.Flow.ID]; exists {
					fmt.Println(time.Now().Sub(start), "UPDATE Event:  ", event.Flow.ID, event)
					state := event.Flow.ProtoInfo.TCP.State
					if state == TCP_CONNTRACK_CLOSE_WAIT || state == TCP_CONNTRACK_LAST_ACK || state == TCP_CONNTRACK_CLOSE {
						fmt.Println("closing?")
					}
					fmt.Println("STATE", event.Flow.ProtoInfo.TCP.State)
					dumpTable()
				}
			default:
				fmt.Println(time.Now().Sub(start), "OTHER EVENT:", event.Flow.ID, event)
			}
		}
	}()

	go func() {
		time.Sleep(180 * time.Second)
		errorChannel <- nil
	}()
	go func() {
		start := time.Now()
		time.Sleep(1 * time.Second)
		exec.Command("bash", "-c", "socat - tcp:10.32.250.2:31337").Run()
		exec.Command("bash", "-c", "socat - tcp:10.32.250.2:31338").Run()
		fmt.Println(time.Now().Sub(start), "Connection closed.")
	}()
	log.Print(<-errorChannel)
}

