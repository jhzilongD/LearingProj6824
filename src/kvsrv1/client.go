package kvsrv

import (
	"6.5840/kvsrv1/rpc"
	kvtest "6.5840/kvtest1"
	tester "6.5840/tester1"
)

type Clerk struct {
	clnt   *tester.Clnt
	server string
}

func MakeClerk(clnt *tester.Clnt, server string) kvtest.IKVClerk {
	ck := &Clerk{clnt: clnt, server: server}
	// You may add code here.
	return ck
}

// Get fetches the current value and version for a key.  It returns
// ErrNoKey if the key does not exist. It keeps trying forever in the
// face of all other errors.
//
// You can send an RPC with code like this:
// ok := ck.clnt.Call(ck.server, "KVServer.Get", &args, &reply)
//
// The types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. Additionally, reply must be passed as a pointer.
func (ck *Clerk) Get(key string) (string, rpc.Tversion, rpc.Err) {
	// 创建请求参数
	args := &rpc.GetArgs{
		Key: key,
	}
	reply := &rpc.GetReply{}
	
	// 重要：Get操作是幂等的（多次执行结果相同），所以可以安全重试
	for {
		// 调用RPC
		ok := ck.clnt.Call(ck.server, "KVServer.Get", args, reply)
		if ok {
			// RPC调用成功，返回结果
			return reply.Value, reply.Version, reply.Err
		}
		// RPC失败（网络问题），继续重试
		// 注意：这里没有退出条件，会一直重试直到成功
	}
}

// Put updates key with value only if the version in the
// request matches the version of the key at the server.  If the
// versions numbers don't match, the server should return
// ErrVersion.  If Put receives an ErrVersion on its first RPC, Put
// should return ErrVersion, since the Put was definitely not
// performed at the server. If the server returns ErrVersion on a
// resend RPC, then Put must return ErrMaybe to the application, since
// its earlier RPC might have been processed by the server successfully
// but the response was lost, and the Clerk doesn't know if
// the Put was performed or not.
//
// You can send an RPC with code like this:
// ok := ck.clnt.Call(ck.server, "KVServer.Put", &args, &reply)
//
// The types of args and reply (including whether they are pointers)
// must match the declared types of the RPC handler function's
// arguments. Additionally, reply must be passed as a pointer.
func (ck *Clerk) Put(key, value string, version rpc.Tversion) rpc.Err {
	// 创建请求参数
	args := &rpc.PutArgs{
		Key:     key,
		Value:   value,
		Version: version,
	}
	reply := &rpc.PutReply{}
	
	// 标记是否是第一次尝试
	firstAttempt := true
	
	for {
		// 调用RPC
		ok := ck.clnt.Call(ck.server, "KVServer.Put", args, reply)
		if ok {
			// RPC调用成功，根据不同的错误类型处理
			switch reply.Err {
			case rpc.OK:
				return rpc.OK
			case rpc.ErrVersion:
				// 版本不匹配
				if firstAttempt {
					// 第一次就失败，确定操作没执行
					return rpc.ErrVersion
				} else {
					// 重试时失败，不确定之前是否执行了
					return rpc.ErrMaybe
				}
			default:
				// 其他错误（如ErrNoKey）
				return reply.Err
			}
		}
		// RPC失败，标记不再是第一次尝试，然后重试
		firstAttempt = false
	}
}
