package pty

import (
	"os"
)

var StdinBytes = make(chan []byte)

func init() {
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				cpy := make([]byte, n)
				copy(cpy, buf[:n])
				StdinBytes <- cpy
			}
			if err != nil {
				return
			}
		}
	}()
}
