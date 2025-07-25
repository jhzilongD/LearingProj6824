package lock

import (
	"6.5840/kvsrv1/rpc"
	"6.5840/kvtest1"
)

type Lock struct {
	// IKVClerk is a go interface for k/v clerks: the interface hides
	// the specific Clerk type of ck but promises that ck supports
	// Put and Get.  The tester passes the clerk in when calling
	// MakeLock().
	ck kvtest.IKVClerk
	// You may add code here
	key      string        // 锁对应的KV存储中的key
	clientID string        // 此Lock实例的唯一标识符
	version  rpc.Tversion  // 当前持有锁时的版本号
}

// The tester calls MakeLock() and passes in a k/v clerk; your code can
// perform a Put or Get by calling lk.ck.Put() or lk.ck.Get().
//
// Use l as the key to store the "lock state" (you would have to decide
// precisely what the lock state is).
func MakeLock(ck kvtest.IKVClerk, l string) *Lock {
	lk := &Lock{
		ck:       ck,
		key:      l,
		clientID: kvtest.RandValue(8), // 为每个Lock实例生成唯一ID
	}
	// 初始化锁：确保key存在，初始值为空字符串（表示未锁定）
	// 如果key已存在，Put会因为版本不匹配而失败，这是正常的
	lk.ck.Put(l, "", 0)
	return lk
}

func (lk *Lock) Acquire() {
	// 循环直到成功获取锁
	for {
		// 获取当前锁的状态
		value, version, err := lk.ck.Get(lk.key)
		
		switch err {
		case rpc.ErrNoKey:
			// key不存在，尝试创建并获取锁
			err = lk.ck.Put(lk.key, lk.clientID, 0)
			if err == rpc.OK {
				lk.version = 1
				return
			}
			// 如果失败（ErrVersion），说明有人抢先创建了，继续重试
			
		case rpc.OK:
			// key存在，检查锁的状态
			switch value {
			case "":
				// 锁未被持有，尝试获取
				err = lk.ck.Put(lk.key, lk.clientID, version)
				if err == rpc.OK {
					lk.version = version + 1
					return
				}
				// 如果失败（ErrVersion或ErrMaybe），继续重试
			case lk.clientID:
				// 我们已经持有锁（支持重入）
				lk.version = version
				return
			default:
				// 锁被其他客户端持有，继续等待
			}
		}
		// 继续重试
	}
}

func (lk *Lock) Release() {
	// 释放锁：将值设为空字符串
	// 使用我们记录的版本号，确保只有持有锁的客户端才能释放
	lk.ck.Put(lk.key, "", lk.version)
	// 注意：我们不检查错误，因为：
	// - 如果成功（OK），锁被释放
	// - 如果失败（ErrVersion），说明锁已经被其他人获取了（可能是超时机制）
	// - 如果失败（ErrMaybe），我们不确定是否释放成功，但这是可接受的
}
