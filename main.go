package main

import (
	"bufio"
	"bytes"
	"context"
	"github.com/pkg/errors"
	"io"
	"log"
	"net"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	lc := new(net.ListenConfig)

	listener, err := lc.Listen(ctx, "tcp", ":8000")
	if err != nil {
		log.Fatalln(err)
	}
	tcpListener := listener.(*net.TCPListener)
	defer tcpListener.Close()

	go func() {
		<-ctx.Done()
		err := tcpListener.Close()
		if err != nil {
			log.Printf("closed listener with %s\n", err)
		}
	}()

	log.Printf("listening at %s\n", tcpListener.Addr().String())

	for {
		conn, err := tcpListener.AcceptTCP()
		if err == io.EOF || errors.Is(err, net.ErrClosed) {
			log.Println("bye")
			break
		} else if err != nil {
			log.Printf("failed to accept new connection: %s\n", err)
			continue
		}
		err = handleConn(ctx, conn)
		if err != nil {
			log.Printf("failed to handle connection: %s\n", err)
			continue
		}
	}
}

func handleConn(ctx context.Context, conn *net.TCPConn) error {
	defer conn.Close()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		select {
		case <-ctx.Done():
		}
		err := conn.Close()
		if err != nil && !errors.Is(err, net.ErrClosed) {
			log.Printf("closed conn with %s\n", err)
		}
	}()

	log.Printf("new connection from %s\n", conn.RemoteAddr().String())

	const bufSize = 32 * 1024
	br := bufio.NewReaderSize(conn, bufSize)

	for { // handle header
		line, err := br.ReadBytes('\n')
		if err == io.EOF {
			log.Println("client closed connection before header complete")
			return nil
		} else if errors.Is(err, net.ErrClosed) {
			log.Println("service shutdown before header complete")
			return nil
		} else if err != nil {
			return errors.WithMessage(err, "failed to read from connection")
		}

		if bytes.Equal(line, []byte("\r\n")) {
			break
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			if len(line) > 1 && line[len(line)-2] == '\r' {
				line = line[:len(line)-2]
			} else {
				line = line[:len(line)-1]
			}
		}
		log.Printf("header: '%s'", line)
	}

	_, err := conn.Write([]byte("HTTP/1.0 200 OK\n\n"))
	if err == io.EOF {
		log.Println("client closed while waiting for OK")
		return nil
	} else if errors.Is(err, net.ErrClosed) {
		log.Println("service shutdown while writing OK")
		return nil
	} else if err != nil {
		return errors.WithMessage(err, "failed to write to connection")
	}

	b := make([]byte, bufSize)

	n := 0
	timeStart := time.Now()

	for { // handle body
		nDelta, err := br.Read(b)
		if err == io.EOF {
			log.Println("end of request body")
			break
		} else if errors.Is(err, net.ErrClosed) {
			log.Println("service shutdown while reading body")
			return nil
		} else if err != nil {
			return errors.WithMessage(err, "failed to read from connection")
		}

		n += nDelta
		log.Printf("read %d bytes\n", nDelta)
	}

	duration := time.Now().Sub(timeStart)
	log.Printf("end of body after %d bytes and %s (%.2f KB/s)\n", n, duration.String(), float64(n)/duration.Seconds()/1000)
	return nil
}
