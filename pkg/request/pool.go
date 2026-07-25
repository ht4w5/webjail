package request

import (
	"sync"
)

const (
	bufferSize = 1024
)

var pool = sync.Pool{
	New: func() any {
		return New(bufferSize)
	},
}

func Get() *Request {
	return pool.Get().(*Request)
}

func Put(req *Request) {
	pool.Put(req)
}
