package main

import (
	"github.com/ti-mo/conntrack"
	"log"
	"strconv"
	"strings"
	"time"
)

type AccountingEntry struct {
	packetsSrcToDst, bytesSrcToDst uint64
	packetsDstToSrc, bytesDstToSrc uint64
	connectionCount                int
	connectionTime                 int64
	openConnections                int
}

var AccountingTable = make(map[string]*AccountingEntry)

func AccountingKey(flow *conntrack.Flow) string {
	s := ProtoLookup(flow.TupleOrig.Proto.Protocol) + ","
	s += flow.TupleOrig.IP.SourceAddress.Mask(SourceGroupMask).String() + ","
	s += flow.TupleOrig.IP.DestinationAddress.Mask(DestGroupMask).String() + ","
	s += strconv.FormatUint(uint64(flow.TupleOrig.Proto.DestinationPort), 10)
	return s
}

func getOrCreateAccountingTableEntry(key string) *AccountingEntry {
	entry := AccountingTable[key]
	if entry == nil {
		entry = &AccountingEntry{}
		AccountingTable[key] = entry
	}
	return entry
}

func AccountTraffic(info *ConnectionInfo) {
	// Is there anything to account?
	if info.packetsSrcToDst == info.packetsSrcToDstAccounted && info.bytesSrcToDst == info.bytesSrcToDstAccounted {
		if info.packetsDstToSrc == info.packetsDstToSrcAccounted && info.bytesDstToSrc == info.bytesDstToSrcAccounted {
			return
		}
	}
	// Account data
	entry := getOrCreateAccountingTableEntry(info.key)
	entry.packetsSrcToDst += info.packetsSrcToDst - info.packetsSrcToDstAccounted
	entry.packetsDstToSrc += info.packetsDstToSrc - info.packetsDstToSrcAccounted
	entry.bytesSrcToDst += info.bytesSrcToDst - info.bytesSrcToDstAccounted
	entry.bytesDstToSrc += info.bytesDstToSrc - info.bytesDstToSrcAccounted
	// Reset connection
	info.packetsSrcToDstAccounted = info.packetsSrcToDst
	info.packetsDstToSrcAccounted = info.packetsDstToSrc
	info.bytesSrcToDstAccounted = info.bytesSrcToDst
	info.bytesDstToSrcAccounted = info.bytesDstToSrc
}

func AccountConnectionClose(info *ConnectionInfo) {
	if !info.connectionTrackingDisabled {
		info.connectionTrackingDisabled = true
		duration := time.Now().Sub(info.start).Milliseconds()
		entry := getOrCreateAccountingTableEntry(info.key)
		entry.connectionCount += 1
		entry.connectionTime += duration
	}
}

func AccountOpenConnection(info *ConnectionInfo) {
	entry := getOrCreateAccountingTableEntry(info.key)
	entry.openConnections += 1
}

func FlushAccountingTableToOutput() {
	start := time.Now()
	size := len(AccountingTable)
	for key, entry := range AccountingTable {
		var line strings.Builder
		line.WriteString(key)
		line.WriteString(",")
		line.WriteString(strconv.FormatUint(entry.packetsSrcToDst, 10))
		line.WriteString(",")
		line.WriteString(strconv.FormatUint(entry.packetsDstToSrc, 10))
		line.WriteString(",")
		line.WriteString(strconv.FormatUint(entry.bytesSrcToDst, 10))
		line.WriteString(",")
		line.WriteString(strconv.FormatUint(entry.bytesDstToSrc, 10))
		line.WriteString(",")
		line.WriteString(strconv.Itoa(entry.connectionCount))
		line.WriteString(",")
		line.WriteString(strconv.FormatInt(entry.connectionTime, 10))
		if TrackOpenConnections {
			line.WriteString(",")
			line.WriteString(strconv.Itoa(entry.openConnections))
		}
		line.WriteString("\n")
		_, err := Output.WriteString(line.String())
		if err != nil {
			log.Fatal("Output write error: ", err)
		}
	}
	// Clear accounting table
	AccountingTable = make(map[string]*AccountingEntry)

	log.Println("[Output] wrote", size, "entries in", time.Now().Sub(start).Milliseconds(), "ms")
}
