// Copyright (C) 2014 Nippon Telegraph and Telephone Corporation.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or
// implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package table

import (
	"bytes"
	"encoding/json"
	"fmt"
	log "github.com/gopher-net/gopher-net/Godeps/_workspace/src/github.com/Sirupsen/logrus"
	"github.com/gopher-net/gopher-net/Godeps/_workspace/src/github.com/tchap/go-patricia/patricia"
	bgp "github.com/gopher-net/gopher-net/third-party/github.com/gobgp/packet"
	"net"
	"reflect"
)

type Table interface {
	createDest(nlri bgp.AddrPrefixInterface) Destination
	GetDestinations() map[string]Destination
	setDestinations(destinations map[string]Destination)
	getDestination(key string) Destination
	setDestination(key string, dest Destination)
	tableKey(nlri bgp.AddrPrefixInterface) string
	validatePath(path Path)
	validateNlri(nlri bgp.AddrPrefixInterface)
	DeleteDestByPeer(*PeerInfo) []Destination
	MarshalJSON() ([]byte, error)
}

type TableDefault struct {
	ROUTE_FAMILY bgp.RouteFamily
	destinations map[string]Destination
	//need SignalBus
}

func NewTableDefault(scope_id int) *TableDefault {
	table := &TableDefault{}
	table.ROUTE_FAMILY = bgp.RF_IPv4_UC
	table.destinations = make(map[string]Destination)
	return table

}

func cidr2prefix(cidr string) patricia.Prefix {
	_, n, _ := net.ParseCIDR(cidr)
	var buffer bytes.Buffer
	for i := 0; i < len(n.IP); i++ {
		buffer.WriteString(fmt.Sprintf("%08b", n.IP[i]))
	}
	ones, _ := n.Mask.Size()
	return patricia.Prefix(buffer.String()[:ones])
}

func (td *TableDefault) MarshalJSON() ([]byte, error) {
	trie := patricia.NewTrie()
	for key, dest := range td.destinations {
		trie.Insert(cidr2prefix(key), dest)
	}

	destList := make([]Destination, 0)
	trie.Visit(func(prefix patricia.Prefix, item patricia.Item) error {
		dest, _ := item.(Destination)
		destList = append(destList, dest)
		return nil
	})

	return json.Marshal(struct {
		Destinations []Destination
	}{
		Destinations: destList,
	})
}

func (td *TableDefault) GetRoutefamily() bgp.RouteFamily {
	return td.ROUTE_FAMILY
}

//Creates destination
//Implements interface
func (td *TableDefault) createDest(nlri *bgp.NLRInfo) Destination {
	//return NewDestination(td, nlri)
	log.Error("CreateDest NotImplementedError")
	return nil
}

func insert(table Table, path Path) Destination {
	var dest Destination

	table.validatePath(path)
	table.validateNlri(path.GetNlri())
	dest = getOrCreateDest(table, path.GetNlri())

	if path.IsWithdraw() {
		// withdraw insert
		dest.addWithdraw(path)
	} else {
		// path insert
		dest.addNewPath(path)
	}
	return dest
}

/*
//Cleans table of any path that do not have any RT in common with interested_rts
// Commented out because it is a VPN-related processing
func (td *TableDefault) cleanUninterestingPaths(interested_rts) int  {
	uninterestingDestCount = 0
	for _, dest := range td.destinations {
		addedWithdraw :=dest.withdrawUnintrestingPaths(interested_rts)
		if addedWithdraw{
			//need _signal_bus.dest_changed(dest)
			uninterestingDestCount += 1
		}
	}
	return uninterestingDestCount
	// need content
}
*/

func (td *TableDefault) DeleteDestByPeer(peerInfo *PeerInfo) []Destination {
	changedDests := make([]Destination, 0)
	for _, dest := range td.destinations {
		newKnownPathList := make([]Path, 0)
		for _, p := range dest.getKnownPathList() {
			if p.getSource() != peerInfo {
				newKnownPathList = append(newKnownPathList, p)
			}
		}
		if len(newKnownPathList) != len(dest.getKnownPathList()) {
			changedDests = append(changedDests, dest)
			dest.setKnownPathList(newKnownPathList)
		}
	}
	return changedDests
}

func deleteDestByNlri(table Table, nlri bgp.AddrPrefixInterface) Destination {
	table.validateNlri(nlri)
	destinations := table.GetDestinations()
	dest := destinations[table.tableKey(nlri)]
	if dest != nil {
		delete(destinations, table.tableKey(nlri))
	}
	return dest
}

func deleteDest(table Table, dest Destination) {
	destinations := table.GetDestinations()
	delete(destinations, table.tableKey(dest.GetNlri()))
}

func (td *TableDefault) validatePath(path Path) {
	if path == nil || path.GetRouteFamily() != td.ROUTE_FAMILY {
		log.Errorf("Invalid path. Expected instance of %s route family path, got %s.", td.ROUTE_FAMILY, path)
	}
	_, attr := path.GetPathAttr(bgp.BGP_ATTR_TYPE_AS_PATH)
	if attr != nil {
		pathParam := attr.(*bgp.PathAttributeAsPath).Value
		for _, as := range pathParam {
			_, y := as.(*bgp.As4PathParam)
			if !y {
				log.Fatal("AsPathParam must be converted to As4PathParam, ", as)
			}
		}
	}

	_, attr = path.GetPathAttr(bgp.BGP_ATTR_TYPE_AS4_PATH)
	if attr != nil {
		log.Fatal("AS4_PATH must be converted to AS_PATH")
	}
}

func (td *TableDefault) validateNlri(nlri bgp.AddrPrefixInterface) {
	if nlri == nil {
		log.Error("Invalid Vpnv4 prefix given.")
	}
}

func getOrCreateDest(table Table, nlri bgp.AddrPrefixInterface) Destination {
	log.Debugf("Table type : %s", reflect.TypeOf(table))
	tableKey := table.tableKey(nlri)
	dest := table.getDestination(tableKey)
	// If destination for given prefix does not exist we create it.
	if dest == nil {
		log.Debugf("dest with key %s is not found", tableKey)
		dest = table.createDest(nlri)
		table.setDestination(tableKey, dest)
	}
	return dest
}

func (td *TableDefault) GetDestinations() map[string]Destination {
	return td.destinations
}
func (td *TableDefault) setDestinations(destinations map[string]Destination) {
	td.destinations = destinations
}
func (td *TableDefault) getDestination(key string) Destination {
	dest, ok := td.destinations[key]
	if ok {
		return dest
	} else {
		return nil
	}
}

func (td *TableDefault) setDestination(key string, dest Destination) {
	td.destinations[key] = dest
}

//Implements interface
func (td *TableDefault) tableKey(nlri bgp.AddrPrefixInterface) string {
	//need Inheritance over ride
	//return &nlri.IPAddrPrefix.IPAddrPrefixDefault.Prefix
	log.Error("CreateDest NotImplementedError")
	return ""
}

/*
* 	Definition of inherited Table interface
 */

type IPv4Table struct {
	*TableDefault
	//need structure
}

func NewIPv4Table(scope_id int) *IPv4Table {
	ipv4Table := &IPv4Table{}
	ipv4Table.TableDefault = NewTableDefault(scope_id)
	ipv4Table.TableDefault.ROUTE_FAMILY = bgp.RF_IPv4_UC
	//need Processing
	return ipv4Table
}

//Creates destination
//Implements interface
func (ipv4t *IPv4Table) createDest(nlri bgp.AddrPrefixInterface) Destination {
	return NewIPv4Destination(nlri)
}

//make tablekey
//Implements interface
func (ipv4t *IPv4Table) tableKey(nlri bgp.AddrPrefixInterface) string {
	switch p := nlri.(type) {
	case *bgp.NLRInfo:
		return p.IPAddrPrefix.IPAddrPrefixDefault.String()
	case *bgp.WithdrawnRoute:
		return p.IPAddrPrefix.IPAddrPrefixDefault.String()
	}
	log.Fatal()
	return ""
}

type IPv6Table struct {
	*TableDefault
	//need structure
}

func NewIPv6Table(scope_id int) *IPv6Table {
	ipv6Table := &IPv6Table{}
	ipv6Table.TableDefault = NewTableDefault(scope_id)
	ipv6Table.TableDefault.ROUTE_FAMILY = bgp.RF_IPv6_UC
	//need Processing
	return ipv6Table
}

//Creates destination
//Implements interface
func (ipv6t *IPv6Table) createDest(nlri bgp.AddrPrefixInterface) Destination {
	return Destination(NewIPv6Destination(nlri))
}

//make tablekey
//Implements interface
func (ipv6t *IPv6Table) tableKey(nlri bgp.AddrPrefixInterface) string {

	addrPrefix := nlri.(*bgp.IPv6AddrPrefix)
	return addrPrefix.IPAddrPrefixDefault.String()

}
