package request

import (
	"sync"

	"github.com/valyala/bytebufferpool"
)

var bytePool = bytebufferpool.Pool{}

var pool = sync.Pool{
	New: func() any {
		req := New()
		req.Path = bytePool.Get()
		req.UserAgent = bytePool.Get()
		return req
	},
}

func Get() *Request {
	return pool.Get().(*Request)
}

func Put(req *Request) {
	bytePool.Put(req.Path)
	bytePool.Put(req.UserAgent)
	req.reset()
	pool.Put(req)
}
