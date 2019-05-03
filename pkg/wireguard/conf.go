// Copyright 2019 the Kilo authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package wireguard

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
)

type section string
type key string

const (
	separator                      = "="
	interfaceSection       section = "Interface"
	peerSection            section = "Peer"
	listenPortKey          key     = "ListenPort"
	allowedIPsKey          key     = "AllowedIPs"
	endpointKey            key     = "Endpoint"
	persistentKeepaliveKey key     = "PersistentKeepalive"
	privateKeyKey          key     = "PrivateKey"
	publicKeyKey           key     = "PublicKey"
)

// Conf represents a WireGuard configuration file.
type Conf struct {
	Interface *Interface
	Peers     []*Peer
}

// Interface represents the `interface` section of a WireGuard configuration.
type Interface struct {
	ListenPort uint32
	PrivateKey []byte
}

// Peer represents a `peer` section of a WireGuard configuration.
type Peer struct {
	AllowedIPs          []*net.IPNet
	Endpoint            *Endpoint
	PersistentKeepalive int
	PublicKey           []byte
}

// Endpoint represents an `endpoint` key of a `peer` section.
type Endpoint struct {
	IP   net.IP
	Port uint32
}

// Parse parses a given WireGuard configuration file and produces a Conf struct.
func Parse(buf []byte) *Conf {
	var (
		active  section
		ai      *net.IPNet
		kv      []string
		c       Conf
		err     error
		iface   *Interface
		i       int
		ip, ip4 net.IP
		k       key
		line, v string
		peer    *Peer
		port    uint64
	)
	s := bufio.NewScanner(bytes.NewBuffer(buf))
	for s.Scan() {
		line = strings.TrimSpace(s.Text())
		// Skip comments.
		if strings.HasPrefix(line, "#") {
			continue
		}
		// Line is a section title.
		if strings.HasPrefix(line, "[") {
			if peer != nil {
				c.Peers = append(c.Peers, peer)
				peer = nil
			}
			if iface != nil {
				c.Interface = iface
				iface = nil
			}
			active = section(strings.TrimSpace(strings.Trim(line, "[]")))
			switch active {
			case interfaceSection:
				iface = new(Interface)
			case peerSection:
				peer = new(Peer)
			}
			continue
		}
		kv = strings.SplitN(line, separator, 2)
		if len(kv) != 2 {
			continue
		}
		k = key(strings.TrimSpace(kv[0]))
		v = strings.TrimSpace(kv[1])
		switch active {
		case interfaceSection:
			switch k {
			case listenPortKey:
				port, err = strconv.ParseUint(v, 10, 32)
				if err != nil {
					continue
				}
				iface.ListenPort = uint32(port)
			case privateKeyKey:
				iface.PrivateKey = []byte(v)
			}
		case peerSection:
			switch k {
			case allowedIPsKey:
				// Reuse string slice.
				kv = strings.Split(v, ",")
				for i = range kv {
					ip, ai, err = net.ParseCIDR(strings.TrimSpace(kv[i]))
					if err != nil {
						continue
					}
					if ip4 = ip.To4(); ip4 != nil {
						ip = ip4
					} else {
						ip = ip.To16()
					}
					ai.IP = ip
					peer.AllowedIPs = append(peer.AllowedIPs, ai)
				}
			case endpointKey:
				// Reuse string slice.
				kv = strings.Split(v, ":")
				if len(kv) != 2 {
					continue
				}
				ip = net.ParseIP(kv[0])
				if ip == nil {
					continue
				}
				port, err = strconv.ParseUint(kv[1], 10, 32)
				if err != nil {
					continue
				}
				if ip4 = ip.To4(); ip4 != nil {
					ip = ip4
				} else {
					ip = ip.To16()
				}
				peer.Endpoint = &Endpoint{
					IP:   ip,
					Port: uint32(port),
				}
			case persistentKeepaliveKey:
				i, err = strconv.Atoi(v)
				if err != nil {
					continue
				}
				peer.PersistentKeepalive = i
			case publicKeyKey:
				peer.PublicKey = []byte(v)
			}
		}
	}
	if peer != nil {
		c.Peers = append(c.Peers, peer)
	}
	if iface != nil {
		c.Interface = iface
	}
	return &c
}

// Bytes renders a WireGuard configuration to bytes.
func (c *Conf) Bytes() ([]byte, error) {
	var err error
	buf := bytes.NewBuffer(make([]byte, 0, 512))
	if c.Interface != nil {
		if err = writeSection(buf, interfaceSection); err != nil {
			return nil, fmt.Errorf("failed to write interface: %v", err)
		}
		if err = writePKey(buf, privateKeyKey, c.Interface.PrivateKey); err != nil {
			return nil, fmt.Errorf("failed to write private key: %v", err)
		}
		if err = writeValue(buf, listenPortKey, strconv.FormatUint(uint64(c.Interface.ListenPort), 10)); err != nil {
			return nil, fmt.Errorf("failed to write listen port: %v", err)
		}
	}
	for i, p := range c.Peers {
		// Add newlines to make the formatting nicer.
		if i == 0 && c.Interface != nil || i != 0 {
			if err = buf.WriteByte('\n'); err != nil {
				return nil, err
			}
		}

		if err = writeSection(buf, peerSection); err != nil {
			return nil, fmt.Errorf("failed to write interface: %v", err)
		}
		if err = writeAllowedIPs(buf, p.AllowedIPs); err != nil {
			return nil, fmt.Errorf("failed to write allowed IPs: %v", err)
		}
		if err = writeEndpoint(buf, p.Endpoint); err != nil {
			return nil, fmt.Errorf("failed to write endpoint: %v", err)
		}
		if err = writeValue(buf, persistentKeepaliveKey, strconv.Itoa(p.PersistentKeepalive)); err != nil {
			return nil, fmt.Errorf("failed to write persistent keepalive: %v", err)
		}
		if err = writePKey(buf, publicKeyKey, p.PublicKey); err != nil {
			return nil, fmt.Errorf("failed to write public key: %v", err)
		}
	}
	return buf.Bytes(), nil
}

// Equal checks if two WireGuare configurations are equivalent.
func (c *Conf) Equal(b *Conf) bool {
	if (c.Interface == nil) != (b.Interface == nil) {
		return false
	}
	if c.Interface != nil {
		if c.Interface.ListenPort != b.Interface.ListenPort || !bytes.Equal(c.Interface.PrivateKey, b.Interface.PrivateKey) {
			return false
		}
	}
	if len(c.Peers) != len(b.Peers) {
		return false
	}
	sortPeers(c.Peers)
	sortPeers(b.Peers)
	for i := range c.Peers {
		if len(c.Peers[i].AllowedIPs) != len(b.Peers[i].AllowedIPs) {
			return false
		}
		sortCIDRs(c.Peers[i].AllowedIPs)
		sortCIDRs(b.Peers[i].AllowedIPs)
		for j := range c.Peers[i].AllowedIPs {
			if c.Peers[i].AllowedIPs[j].String() != b.Peers[i].AllowedIPs[j].String() {
				return false
			}
		}
		if (c.Peers[i].Endpoint == nil) != (b.Peers[i].Endpoint == nil) {
			return false
		}
		if c.Peers[i].Endpoint != nil {
			if !c.Peers[i].Endpoint.IP.Equal(b.Peers[i].Endpoint.IP) || c.Peers[i].Endpoint.Port != b.Peers[i].Endpoint.Port {
				return false
			}
		}
		if c.Peers[i].PersistentKeepalive != b.Peers[i].PersistentKeepalive || !bytes.Equal(c.Peers[i].PublicKey, b.Peers[i].PublicKey) {
			return false
		}
	}
	return true
}

func sortPeers(peers []*Peer) {
	sort.Slice(peers, func(i, j int) bool {
		if bytes.Compare(peers[i].PublicKey, peers[j].PublicKey) < 0 {
			return true
		}
		return false
	})
}

func sortCIDRs(cidrs []*net.IPNet) {
	sort.Slice(cidrs, func(i, j int) bool {
		return cidrs[i].String() < cidrs[j].String()
	})
}

func writeAllowedIPs(buf *bytes.Buffer, ais []*net.IPNet) error {
	if len(ais) == 0 {
		return nil
	}
	var err error
	if err = writeKey(buf, allowedIPsKey); err != nil {
		return err
	}
	for i := range ais {
		if i != 0 {
			if _, err = buf.WriteString(", "); err != nil {
				return err
			}
		}
		if _, err = buf.WriteString(ais[i].String()); err != nil {
			return err
		}
	}
	if err = buf.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func writePKey(buf *bytes.Buffer, k key, b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var err error
	if err = writeKey(buf, k); err != nil {
		return err
	}
	if _, err = buf.Write(b); err != nil {
		return err
	}
	if err = buf.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func writeValue(buf *bytes.Buffer, k key, v string) error {
	var err error
	if err = writeKey(buf, k); err != nil {
		return err
	}
	if _, err = buf.WriteString(v); err != nil {
		return err
	}
	if err = buf.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func writeEndpoint(buf *bytes.Buffer, e *Endpoint) error {
	if e == nil {
		return nil
	}
	var err error
	if err = writeKey(buf, endpointKey); err != nil {
		return err
	}
	if _, err = buf.WriteString(e.IP.String()); err != nil {
		return err
	}
	if err = buf.WriteByte(':'); err != nil {
		return err
	}
	if _, err = buf.WriteString(strconv.FormatUint(uint64(e.Port), 10)); err != nil {
		return err
	}
	if err = buf.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func writeSection(buf *bytes.Buffer, s section) error {
	var err error
	if err = buf.WriteByte('['); err != nil {
		return err
	}
	if _, err = buf.WriteString(string(s)); err != nil {
		return err
	}
	if err = buf.WriteByte(']'); err != nil {
		return err
	}
	if err = buf.WriteByte('\n'); err != nil {
		return err
	}
	return nil
}

func writeKey(buf *bytes.Buffer, k key) error {
	var err error
	if _, err = buf.WriteString(string(k)); err != nil {
		return err
	}
	if _, err = buf.WriteString(" = "); err != nil {
		return err
	}
	return nil
}
