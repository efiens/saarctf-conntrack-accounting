package main

import (
	"github.com/mdlayher/netlink"
	"github.com/ti-mo/conntrack"
	"github.com/ti-mo/netfilter"
	"log"
	"time"
)

type ConnectionInfo struct {
	key                                              string
	packetsSrcToDst, bytesSrcToDst                   uint64
	packetsDstToSrc, bytesDstToSrc                   uint64
	packetsSrcToDstAccounted, bytesSrcToDstAccounted uint64
	packetsDstToSrcAccounted, bytesDstToSrcAccounted uint64
	connectionTrackingDisabled                       bool // connection is untrackable or closed
	start                                            time.Time
}

var connections = make(map[uint32]*ConnectionInfo)

func accountOpenConnections() {
	for _, info := range connections {
		if !info.connectionTrackingDisabled {
			AccountOpenConnection(info)
		}
	}
}

func handleDump(flows []conntrack.Flow) {
	if len(flows) == 0 {
		return
	}
	start := time.Now()
	var interestingFlowCounter int
	for _, flow := range flows {
		if FlowIsInteresting(&flow) {
			interestingFlowCounter++
			if info, ok := connections[flow.ID]; ok {
				// We know this flow, update its stats
				if flow.CountersOrig.Packets != 0 && flow.CountersOrig.Bytes != 0 {
					info.packetsSrcToDst = flow.CountersOrig.Packets
					info.bytesSrcToDst = flow.CountersOrig.Bytes
				}
				if flow.CountersReply.Packets != 0 && flow.CountersReply.Bytes != 0 {
					info.packetsDstToSrc = flow.CountersReply.Packets
					info.bytesDstToSrc = flow.CountersReply.Bytes
				}
				AccountTraffic(info)
			} else {
				// We don't know this flow, so we can't do connection tracking.
				// But we can count future traffic if accounting is enabled.
				if flow.CountersOrig.Packets != 0 || flow.CountersReply.Packets != 0 {
					connections[flow.ID] = &ConnectionInfo{
						key:                        AccountingKey(&flow),
						packetsSrcToDstAccounted:   flow.CountersOrig.Packets,
						bytesSrcToDstAccounted:     flow.CountersOrig.Bytes,
						packetsDstToSrcAccounted:   flow.CountersReply.Packets,
						bytesDstToSrcAccounted:     flow.CountersReply.Bytes,
						connectionTrackingDisabled: true,
					}
				}
			}
		}
	}
	log.Println("[Dump] Handled", interestingFlowCounter, "flows out of", len(flows), "in", time.Now().Sub(start).Milliseconds(), "ms")
}

func handleNewFlow(flow *conntrack.Flow) {
	connections[flow.ID] = &ConnectionInfo{
		key:                        AccountingKey(flow),
		start:                      time.Now(),
		connectionTrackingDisabled: flow.TupleOrig.Proto.Protocol != PROTO_TCP && flow.TupleOrig.Proto.Protocol != PROTO_DCCP && flow.TupleOrig.Proto.Protocol != PROTO_SCTP,
	}
}

func handleDestroyFlow(flow *conntrack.Flow) {
	if info, ok := connections[flow.ID]; ok {
		delete(connections, flow.ID)
		if flow.CountersOrig.Packets != 0 && flow.CountersOrig.Bytes != 0 {
			info.packetsSrcToDst = flow.CountersOrig.Packets
			info.bytesSrcToDst = flow.CountersOrig.Bytes
		}
		if flow.CountersReply.Packets != 0 && flow.CountersReply.Bytes != 0 {
			info.packetsDstToSrc = flow.CountersReply.Packets
			info.bytesDstToSrc = flow.CountersReply.Bytes
		}
		AccountTraffic(info)
		if !info.connectionTrackingDisabled {
			AccountConnectionClose(info)
		}
	}
}

func handleTerminateFlow(flow *conntrack.Flow) {
	if info, ok := connections[flow.ID]; ok {
		if !info.connectionTrackingDisabled {
			AccountConnectionClose(info)
		}
	}
}

func handleConntrackEvent(event conntrack.Event) {
	switch event.Type {
	case conntrack.EventNew:
		handleNewFlow(event.Flow)
	case conntrack.EventDestroy:
		handleDestroyFlow(event.Flow)
	case conntrack.EventUpdate:
		// Check if we know this flow and should terminate it
		if event.Flow.TupleOrig.Proto.Protocol == PROTO_TCP {
			state := event.Flow.ProtoInfo.TCP.State
			if state == TCP_CONNTRACK_CLOSE_WAIT || state == TCP_CONNTRACK_LAST_ACK || state == TCP_CONNTRACK_CLOSE {
				handleTerminateFlow(event.Flow)
			}
		}
	}
}

func GetConntrackEvents() (chan conntrack.Event, chan error) {
	conn, err := conntrack.Dial(nil)
	if err != nil {
		log.Fatal(err)
	}

	eventChannel := make(chan conntrack.Event, 1024)
	errorChannel, err := conn.Listen(eventChannel, 4, netfilter.GroupsCT)
	if err != nil {
		log.Fatal(err)
	}

	err = conn.SetOption(netlink.ListenAllNSID, true)
	if err != nil {
		log.Fatal(err)
	}
	return eventChannel, errorChannel
}

func nextTimestamp(interval int64) int64 {
	return (time.Now().Unix()/interval)*interval + interval
}

func runDumping(channel chan []conntrack.Flow, timestamp int64) {
	time.Sleep(time.Unix(timestamp, 0).Sub(time.Now()))

	start := time.Now()
	// Create connection to conntrack
	conn, err := conntrack.Dial(nil)
	if err != nil {
		log.Fatal("Conntrack dial:", err)
	}
	defer conn.Close()
	// Query dumps
	flows, err := conn.DumpFilter(conntrack.Filter{Mark: 0, Mask: 0})
	if err != nil {
		log.Fatal("DumpFilter:", err)
	}
	// Transmit
	channel <- flows
	log.Println("[Dump] Received", len(flows), "conntrack table entries in", time.Now().Sub(start).Milliseconds(), "ms")
}

func GetDumpingChannel() chan []conntrack.Flow {
	channel := make(chan []conntrack.Flow, 1)
	go runDumping(channel, time.Now().Unix())
	return channel
}
