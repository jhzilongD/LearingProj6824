# Lab 3A - Raft领导者选举 完整总结

## 一、核心概念理解

### 1. Raft的三种角色
```
Follower（跟随者）→ Candidate（候选人）→ Leader（领导者）
    ↑                                          ↓
    ←────────────── 发现更高任期 ───────────────↘
```

- **Follower**：普通节点，接受Leader的心跳
- **Candidate**：竞选中的节点，请求其他节点投票
- **Leader**：负责处理所有客户端请求，发送心跳维持权威

### 2. 任期（Term）机制
```
Term 0 → Term 1 → Term 2 → Term 3 ...
         选举1     选举2     选举3
```
- 逻辑时钟，标识"第几代Leader"
- 每次选举开始，任期+1
- 发现更高任期 → 立即更新并变成Follower

### 3. 两个关键超时
- **心跳间隔**：50ms（Leader发送心跳的频率）
- **选举超时**：150-300ms随机（Follower多久没收到心跳就开始选举）

## 二、代码实现结构

### 1. 数据结构
```go
type Raft struct {
    // 基础字段
    mu        sync.Mutex
    peers     []*labrpc.ClientEnd
    me        int
    
    // 核心状态
    currentTerm int                  // 当前任期
    votedFor    int                  // 本任期投票给谁（-1表示未投票）
    state       State                // 当前角色
    
    // 时间管理
    lastHeartbeat   time.Time        // 上次收到心跳时间
    electionTimeout time.Duration    // 随机的选举超时
    
    // 选举统计
    votesReceived int               // 当前选举收到的票数
}
```

### 2. 两个RPC

**RequestVote RPC**（请求投票）
```go
type RequestVoteArgs struct {
    Term        int  // 候选人的任期
    CandidateId int  // 候选人ID
}

type RequestVoteReply struct {
    Term        int  // 当前任期（用于更新候选人）
    VoteGranted bool // 是否投票
}
```

**AppendEntries RPC**（心跳/日志复制）
```go
type AppendEntriesArgs struct {
    Term     int  // Leader的任期
    LeaderId int  // Leader的ID
}

type AppendEntriesReply struct {
    Term    int  // 当前任期
    Success bool // 是否成功
}
```

### 3. 核心流程

**选举流程**：
```go
1. checkElectionTimeout() 
   → 检测到超时
   → Follower变Candidate
   → Term++
   → 投票给自己
   → 调用startElection()

2. startElection()
   → 并发向所有节点发送RequestVote
   → 收集投票
   → 超过半数 → 变成Leader

3. sendHeartbeats()
   → Leader定期发送AppendEntries
   → 维持Leader地位
```

## 三、关键设计决策

### 1. 随机选举超时
```go
func (rf *Raft) resetElectionTimeout() {
    rf.electionTimeout = time.Duration(150+rand.Intn(150)) * time.Millisecond
    rf.lastHeartbeat = time.Now()
}
```
**为什么重要**：避免多个节点同时发起选举导致分票

### 2. 投票规则
```go
// RequestVote处理
if args.Term < rf.currentTerm {
    return  // 拒绝过期的候选人
}

if args.Term > rf.currentTerm {
    rf.currentTerm = args.Term
    rf.votedFor = -1
    rf.state = Follower
}

if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
    rf.votedFor = args.CandidateId
    reply.VoteGranted = true
    rf.resetElectionTimeout()  // 重要！避免自己也发起选举
}
```

### 3. 状态转换
```go
// 任何时候发现更高任期 → Follower
if reply.Term > rf.currentTerm {
    rf.currentTerm = reply.Term
    rf.state = Follower
    rf.votedFor = -1
    rf.resetElectionTimeout()
}
```

## 四、遇到的问题和解决方案

### 问题1：所有节点同时选举
**原因**：固定的选举超时（200ms）  
**解决**：每个节点随机150-300ms超时

### 问题2：Candidate不断重新开始选举
**原因**：
```go
// 错误的实现
case Candidate:
    rf.startElection()  // 每个ticker都调用，重置已收到的票数！
```
**解决**：
```go
// 正确的实现
case Candidate:
    rf.checkElectionTimeout()  // 只有超时才开始新一轮
```

### 问题3：并发安全
**解决**：
- 所有共享状态访问都加锁
- RPC调用在锁外进行（避免死锁）
- 投票统计在锁内进行（避免竞态）

## 五、Ticker设计
```go
func (rf *Raft) ticker() {
    for !rf.killed() {
        switch rf.state {
        case Follower, Candidate:
            rf.checkElectionTimeout()
        case Leader:
            rf.sendHeartbeats()
        }
        
        // 不同角色不同间隔
        if rf.state == Leader {
            time.Sleep(50ms)  // 心跳间隔
        } else {
            time.Sleep(50-150ms)  // 检查间隔
        }
    }
}
```

## 六、测试要点

1. **TestInitialElection3A**：能否选出第一个Leader
2. **TestReElection3A**：Leader失败后能否选出新Leader  
3. **TestManyElections3A**：多次选举是否正常

## 七、Lab 3A的核心教训

1. **事件驱动 vs 周期轮询**：选举是事件驱动的，不是简单的周期操作
2. **随机化的重要性**：避免对称性导致的活锁
3. **状态机思维**：清晰的状态转换规则
4. **并发编程**：正确处理锁和goroutine

## 八、重要细节

### 1. 为什么选举超时要比心跳间隔长？
- 心跳间隔：50ms
- 选举超时：150-300ms
- 这给网络一些容错空间，避免偶尔丢失一个心跳就重新选举

### 2. 收到RequestVote时为什么要重置选举超时？
```go
if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
    rf.votedFor = args.CandidateId
    reply.VoteGranted = true
    rf.resetElectionTimeout()  // 这里！
}
```
避免投票给某个候选人后，自己也发起选举，导致分票。

### 3. 任期的作用
- 逻辑时钟，解决"谁是最新的Leader"问题
- 保证每个任期最多一个Leader
- 检测过期的消息

### 4. 并发投票收集
```go
// 每个投票请求都在独立的goroutine中
go func(server int) {
    reply := RequestVoteReply{}
    ok := rf.sendRequestVote(server, &args, &reply)
    if ok {
        rf.mu.Lock()
        // 在锁内更新状态
        if reply.VoteGranted {
            rf.votesReceived++
            if rf.votesReceived > len(rf.peers)/2 {
                rf.state = Leader
            }
        }
        rf.mu.Unlock()
    }
}(i)
```

 