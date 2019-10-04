package connection_pool

import (
	"context"
	"github.com/ihciah/rabbit-tcp/block"
	"github.com/ihciah/rabbit-tcp/connection"
	"github.com/ihciah/rabbit-tcp/tunnel_pool"
	"log"
	"os"
)

const (
	SendQueueSize = 24
)

type ConnectionPool struct {
	connectionMapping map[uint32]connection.Connection
	tunnelPool        *tunnel_pool.TunnelPool
	sendQueue         chan block.Block
	logger            *log.Logger

	ctx    context.Context
	cancel context.CancelFunc
}

func NewConnectionPool(pool *tunnel_pool.TunnelPool) ConnectionPool {
	ctx, cancel := context.WithCancel(context.Background())
	cp := ConnectionPool{
		connectionMapping: make(map[uint32]connection.Connection),
		tunnelPool:        pool,
		sendQueue:         make(chan block.Block, SendQueueSize),
		logger:            log.New(os.Stdout, "[ConnectionPool]", log.LstdFlags),
		ctx:               ctx,
		cancel:            cancel,
	}
	cp.logger.Println("ConnectionPool created.")
	go cp.SendRelay()
	go cp.RecvRelay()
	return cp
}

// Create InboundConnection, and it to ConnectionPool and return
func (cp *ConnectionPool) NewInboundConnection() connection.Connection {
	c := connection.NewInboundConnection(cp.sendQueue)
	cp.AddConnection(c)
	return c
}

// Create OutboundConnection, and it to ConnectionPool and return
func (cp *ConnectionPool) NewOutboundConnection(connectionID uint32) connection.Connection {
	c := connection.NewOutboundConnectionWithID(connectionID, cp.sendQueue)
	return c
}

func (cp *ConnectionPool) AddConnection(conn connection.Connection) {
	// TODO: thread safe
	cp.connectionMapping[conn.GetConnectionID()] = conn
	go conn.Daemon(conn)
}

func (cp *ConnectionPool) RemoveConnection(conn connection.Connection) {
	// TODO: thread safe
	if _, ok := cp.connectionMapping[conn.GetConnectionID()]; ok {
		delete(cp.connectionMapping, conn.GetConnectionID())
		conn.StopDaemon()
	}
}

// Deliver blocks from tunnelPool channel to specified connections
func (cp *ConnectionPool) RecvRelay() {
	cp.logger.Println("RecvRelay started.")
	for {
		select {
		case blk := <-cp.tunnelPool.GetRecvQueue():
			connID := blk.ConnectionID
			var conn connection.Connection
			var ok bool
			if conn, ok = cp.connectionMapping[connID]; !ok {
				conn = cp.NewOutboundConnection(blk.ConnectionID)
				cp.AddConnection(conn)
				cp.logger.Println("Connection created and added to connectionPool.")
			}
			conn.RecvBlock(blk)
			cp.logger.Printf("Block %d(type: %d) put to connRecvQueue.\n", blk.BlockID, blk.Type)
		case <-cp.ctx.Done():
			return
		}
	}
}

// Deliver blocks from connPool's sendQueue to tunnelPool
// TODO: Maybe QOS can be implemented here
func (cp *ConnectionPool) SendRelay() {
	cp.logger.Println("SendRelay started.")
	for {
		select {
		case blk := <-cp.sendQueue:
			cp.tunnelPool.GetSendQueue() <- blk
			cp.logger.Printf("Block %d(type: %d) put to connSendQueue.\n", blk.BlockID, blk.Type)
		case <-cp.ctx.Done():
			return
		}
	}
}

func (cp *ConnectionPool) StopRelay() {
	cp.logger.Println("Relay stopped.")
	cp.cancel()
}
