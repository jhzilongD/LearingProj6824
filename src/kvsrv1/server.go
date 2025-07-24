package kvsrv

import (
	"log"
	"sync"

	"6.5840/kvsrv1/rpc"
	"6.5840/labrpc"
	tester "6.5840/tester1"
)

const Debug = false

func DPrintf(format string, a ...interface{}) (n int, err error) {
	if Debug {
		log.Printf(format, a...)
	}
	return
}

type KVServer struct {
	mu sync.Mutex

	// Your definitions here.
	data     map[string]string      // 存储 key -> value
	versions map[string]rpc.Tversion // 存储 key -> version
}

func MakeKVServer() *KVServer {
	kv := &KVServer{}
	// Your code here.
	kv.data = make(map[string]string)
	kv.versions = make(map[string]rpc.Tversion)
	return kv
}

// Get returns the value and version for args.Key, if args.Key
// exists. Otherwise, Get returns ErrNoKey.
func (kv *KVServer) Get(args *rpc.GetArgs, reply *rpc.GetReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	
	// 检查key是否存在
	value, exists := kv.data[args.Key]
	if !exists {
		reply.Err = rpc.ErrNoKey
		return
	}
	
	// key存在，返回value和version
	reply.Value = value
	reply.Version = kv.versions[args.Key]
	reply.Err = rpc.OK
}

// Update the value for a key if args.Version matches the version of
// the key on the server. If versions don't match, return ErrVersion.
// If the key doesn't exist, Put installs the value if the
// args.Version is 0, and returns ErrNoKey otherwise.
func (kv *KVServer) Put(args *rpc.PutArgs, reply *rpc.PutReply) {
	// Your code here.
	kv.mu.Lock()
	defer kv.mu.Unlock()
	
	// 检查key是否存在
	currentVersion, exists := kv.versions[args.Key]
	
	if exists {
		// key存在，检查版本是否匹配
		if args.Version != currentVersion {
			reply.Err = rpc.ErrVersion
			return
		}
		// 版本匹配，更新value和version
		kv.data[args.Key] = args.Value
		kv.versions[args.Key] = currentVersion + 1
		reply.Err = rpc.OK
	} else {
		// key不存在
		if args.Version == 0 {
			// 版本为0，创建新的键值对
			kv.data[args.Key] = args.Value
			kv.versions[args.Key] = 1  // 新key的初始版本为1
			reply.Err = rpc.OK
		} else {
			// 版本不为0，但key不存在
			reply.Err = rpc.ErrNoKey
		}
	}
}

// You can ignore Kill() for this lab
func (kv *KVServer) Kill() {
}

// You can ignore all arguments; they are for replicated KVservers
func StartKVServer(ends []*labrpc.ClientEnd, gid tester.Tgid, srv int, persister *tester.Persister) []tester.IService {
	kv := MakeKVServer()
	return []tester.IService{kv}
}
