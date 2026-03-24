package udpx

import (
	"context"
	"net"
)

type Packet interface {
	Payload() []byte
	RemoteAddr() net.Addr
	LocalAddr() net.Addr
	Reply(context.Context, []byte) (int, error)
	WriteTo(context.Context, []byte, net.Addr) (int, error)
	PacketConn() net.PacketConn
}

type packetEntity struct {
	payload    []byte
	remote     net.Addr
	local      net.Addr
	packetConn net.PacketConn
	onWrite    func(int)
}

func newPacket(payload []byte, remote, local net.Addr, packetConn net.PacketConn, onWrite func(int)) Packet {
	return &packetEntity{
		payload:    payload,
		remote:     remote,
		local:      local,
		packetConn: packetConn,
		onWrite:    onWrite,
	}
}

func (p *packetEntity) Payload() []byte {
	return p.payload
}

func (p *packetEntity) RemoteAddr() net.Addr {
	return p.remote
}

func (p *packetEntity) LocalAddr() net.Addr {
	return p.local
}

func (p *packetEntity) Reply(ctx context.Context, data []byte) (int, error) {
	return p.WriteTo(ctx, data, p.remote)
}

func (p *packetEntity) WriteTo(ctx context.Context, data []byte, addr net.Addr) (int, error) {
	if err := validateContext(ctx); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	n, err := p.packetConn.WriteTo(data, addr)
	if n > 0 && p.onWrite != nil {
		p.onWrite(n)
	}
	return n, err
}

func (p *packetEntity) PacketConn() net.PacketConn {
	return p.packetConn
}
