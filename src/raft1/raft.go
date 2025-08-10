package raft

// The file raftapi/raft.go defines the interface that raft must
// expose to servers (or the tester), but see comments below for each
// of these functions for more details.
//
// Make() creates a new raft peer that implements the raft interface.

import (
	//	"bytes"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	//	"6.5840/labgob"
	"6.5840/labrpc"
	"6.5840/raftapi"
	tester "6.5840/tester1"
)

// 定义服务器的三种状态
type State int

const (
	Follower  State = iota // 跟随者
	Candidate              // 候选人
	Leader                 // 领导者
)

// 日志条目结构
type LogEntry struct {
	Command interface{} // 要应用到状态机的命令
	Term    int         // 接收时的任期号
}

// A Go object implementing a single Raft peer.
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *tester.Persister   // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()

	// Your data here (3A, 3B, 3C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.

	// ==== 持久状态 (重启后必须保持) ====
	currentTerm int        // 服务器已知最新任期（初始为0，递增）
	votedFor    int        // 本任期投票给了谁（-1表示还没投票）
	log         []LogEntry // 日志条目数组；索引0有一个虚拟条目，真实日志从索引1开始

	// ==== 易失状态 (重启时重置) ====
	commitIndex int // 已知已提交的最高日志条目索引（初始为0，递增）
	lastApplied int // 已应用到状态机的最高日志条目索引（初始为0，递增）

	// ==== Leader易失状态 (选举后重新初始化) ====
	nextIndex  []int // 对每个服务器，要发送的下一个日志条目索引（初始为leader最后日志索引+1）
	matchIndex []int // 对每个服务器，已知已复制的最高日志条目索引（初始为0，递增）

	// ==== 选举相关状态 ====
	state State // 当前状态：Follower、Candidate或Leader

	// ==== 时间相关 ====
	lastHeartbeat   time.Time     // 上次收到Leader心跳的时间
	electionTimeout time.Duration // 随机的选举超时时间

	// ==== 应用层通信 ====
	applyCh chan raftapi.ApplyMsg // 用于向上层应用发送已提交的命令

	// ==== 选举统计 ====
	votesReceived int // 当前选举中收到的票数
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	term := rf.currentTerm
	isleader := rf.state == Leader

	return term, isleader
}

// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
// before you've implemented snapshots, you should pass nil as the
// second argument to persister.Save().
// after you've implemented snapshots, pass the current snapshot
// (or nil if there's not yet a snapshot).
func (rf *Raft) persist() {
	// Your code here (3C).
	// Example:
	// w := new(bytes.Buffer)
	// e := labgob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// raftstate := w.Bytes()
	// rf.persister.Save(raftstate, nil)
}

// restore previously persisted state.
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (3C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := labgob.NewDecoder(r)
	// var xxx
	// var yyy
	// if d.Decode(&xxx) != nil ||
	//    d.Decode(&yyy) != nil {
	//   error...
	// } else {
	//   rf.xxx = xxx
	//   rf.yyy = yyy
	// }
}

// how many bytes in Raft's persisted log?
func (rf *Raft) PersistBytes() int {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.persister.RaftStateSize()
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (3D).

}

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (3A, 3B).
	Term         int // 候选人的任期号
	CandidateId  int // 候选人的ID（谁在请求投票）
	LastLogIndex int // 候选人最后日志条目的索引
	LastLogTerm  int // 候选人最后日志条目的任期
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int  // 当前任期号（用于候选人更新自己）
	VoteGranted bool // true表示给候选人投票了
}

// AppendEntries RPC arguments structure (3B支持日志复制)
type AppendEntriesArgs struct {
	// 基本信息
	Term     int // Leader的任期号
	LeaderId int // Leader的ID，让Follower知道谁是Leader
	
	// 日志一致性检查
	PrevLogIndex int // 新日志条目前一条的索引
	PrevLogTerm  int // 新日志条目前一条的任期
	
	// 日志数据
	Entries []LogEntry // 要存储的日志条目（心跳时为空；为提高效率可能发送多个）
	
	// 提交信息
	LeaderCommit int // Leader的commitIndex
}

// AppendEntries RPC reply structure
type AppendEntriesReply struct {
	Term    int  // 当前任期号，用于Leader更新自己
	Success bool // 如果Follower包含索引为prevLogIndex且任期为prevLogTerm的条目，返回true
}

// example RequestVote RPC handler.
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (3A, 3B).
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 返回自己的任期，让候选人知道
	reply.Term = rf.currentTerm
	reply.VoteGranted = false // 默认不投票

	// 规则1：如果候选人的任期小于自己的，拒绝投票
	if args.Term < rf.currentTerm {
		return
	}

	// 规则2：如果候选人的任期大于自己的，更新任期并变成Follower
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1 // 新任期，重置投票
		rf.state = Follower
	}

	// 规则3：检查是否可以投票
	// 条件：这一任期还没投票（votedFor == -1）或者已经投给了这个候选人
	if rf.votedFor == -1 || rf.votedFor == args.CandidateId {
		// 规则4（3B新增）：检查候选人的日志是否至少和自己一样新
		// 获取自己最后一个日志条目的信息
		lastLogIndex := len(rf.log) - 1
		lastLogTerm := 0
		if lastLogIndex > 0 {
			lastLogTerm = rf.log[lastLogIndex].Term
		}
		
		// 判断候选人的日志是否至少和自己一样新
		logIsAtLeastAsUpToDate := false
		if args.LastLogTerm > lastLogTerm {
			// 候选人最后条目的任期更大，更新
			logIsAtLeastAsUpToDate = true
		} else if args.LastLogTerm == lastLogTerm {
			// 任期相同，比较长度
			if args.LastLogIndex >= lastLogIndex {
				// 候选人的日志至少一样长，足够新
				logIsAtLeastAsUpToDate = true
			}
		}
		
		// 只有候选人的日志足够新才投票
		if logIsAtLeastAsUpToDate {
			// 投票！
			rf.votedFor = args.CandidateId
			reply.VoteGranted = true
			// 重置选举超时（因为听到了候选人的声音）
			rf.resetElectionTimeout()
			DPrintf("Server %d voted for %d", rf.me, args.CandidateId)
		} else {
			DPrintf("Server %d rejected %d: candidate's log not up-to-date", rf.me, args.CandidateId)
		}
	}
}

// AppendEntries RPC handler (3B支持日志复制)
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	DPrintf("Server %d received AppendEntries from %d: term=%d, prevLogIndex=%d, entries=%d", 
		rf.me, args.LeaderId, args.Term, args.PrevLogIndex, len(args.Entries))

	// 步骤1: 检查任期 - 最基本的安全检查
	reply.Term = rf.currentTerm
	reply.Success = false

	if args.Term < rf.currentTerm {
		DPrintf("Server %d rejects: stale term %d < %d", rf.me, args.Term, rf.currentTerm)
		return
	}

	// 如果Leader任期更高，更新自己
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.votedFor = -1
		rf.state = Follower
	}

	// 承认Leader，重置选举超时
	rf.state = Follower
	rf.resetElectionTimeout()

	// 步骤2: 检查日志一致性
	if args.PrevLogIndex > 0 {
		// 检查是否有prevLogIndex这个索引
		if args.PrevLogIndex >= len(rf.log) {
			DPrintf("Server %d rejects: prevLogIndex %d >= log length %d", 
				rf.me, args.PrevLogIndex, len(rf.log))
			return
		}
		// 检查prevLogIndex处的任期是否匹配
		if rf.log[args.PrevLogIndex].Term != args.PrevLogTerm {
			DPrintf("Server %d rejects: term mismatch at index %d: have %d, want %d", 
				rf.me, args.PrevLogIndex, rf.log[args.PrevLogIndex].Term, args.PrevLogTerm)
			return
		}
	}

	// 步骤3&4: 处理冲突并添加新条目
	if len(args.Entries) > 0 {
		// 找到插入位置
		insertIndex := args.PrevLogIndex + 1
		
		// 删除冲突的条目及后续条目（你刚才选择的B选项！）
		rf.log = rf.log[:insertIndex]
		
		// 添加新条目
		rf.log = append(rf.log, args.Entries...)
		
		DPrintf("Server %d added %d entries, log length now %d", 
			rf.me, len(args.Entries), len(rf.log))
	}

	// 步骤5: 更新commitIndex
	if args.LeaderCommit > rf.commitIndex {
		// commitIndex = min(leaderCommit, 最新条目索引)
		newCommitIndex := args.LeaderCommit
		lastLogIndex := len(rf.log) - 1
		if newCommitIndex > lastLogIndex {
			newCommitIndex = lastLogIndex
		}
		rf.commitIndex = newCommitIndex
		DPrintf("Server %d updated commitIndex to %d", rf.me, rf.commitIndex)
	}

	reply.Success = true
}

// example code to send a suo RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

// 发送AppendEntries RPC
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 1. 检查身份：只有Leader才能接收新命令
	if rf.state != Leader {
		return -1, -1, false
	}

	// 2. 创建新的日志条目
	newEntry := LogEntry{
		Command: command,
		Term:    rf.currentTerm,
	}

	// 3. 添加到自己的日志
	rf.log = append(rf.log, newEntry)
	index := len(rf.log) - 1 // 新条目的索引

	DPrintf("Leader %d received new command at index %d, term %d", 
		rf.me, index, rf.currentTerm)

	// 4. 返回信息给上层应用
	// index: 如果这个命令被提交，它会出现在这个索引
	// term: 当前任期
	// isLeader: true表示这个服务器认为自己是Leader
	return index, rf.currentTerm, true

	// 注意：我们不在这里发送RPC！
	// 让现有的心跳机制（sendHeartbeats）负责复制这个新条目
}

// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// 生成随机的选举超时时间（150-300ms）
func (rf *Raft) resetElectionTimeout() {
	rf.electionTimeout = time.Duration(150+rand.Intn(150)) * time.Millisecond
	rf.lastHeartbeat = time.Now()
	DPrintf("Server %d reset election timeout to %v", rf.me, rf.electionTimeout)
}

// Follower检查是否超时需要开始选举
func (rf *Raft) checkElectionTimeout() {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 计算上次心跳到现在过了多久
	elapsed := time.Since(rf.lastHeartbeat)

	// 使用每个服务器自己的随机超时
	if elapsed > rf.electionTimeout {
		// 变成候选人！
		rf.state = Candidate
		rf.currentTerm++              // 开始新任期
		rf.votedFor = rf.me           // 投票给自己
		rf.resetElectionTimeout()     // 重置超时时间

		DPrintf("Server %d timeout after %v, becoming candidate in term %d", 
			rf.me, elapsed, rf.currentTerm)
		
		// 立即发起选举（不要等下一个ticker）
		go rf.startElection()
	}
}

// Candidate发起选举
func (rf *Raft) startElection() {
	rf.mu.Lock()

	// 重置投票计数，给自己投一票
	rf.votesReceived = 1

	// 获取自己最后一个日志条目的信息
	lastLogIndex := len(rf.log) - 1
	lastLogTerm := 0
	if lastLogIndex > 0 {
		lastLogTerm = rf.log[lastLogIndex].Term
	}

	// 准备请求参数
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastLogIndex: lastLogIndex,
		LastLogTerm:  lastLogTerm,
	}
	
	DPrintf("Server %d starting election for term %d", rf.me, rf.currentTerm)

	rf.mu.Unlock()

	// 向所有其他服务器请求投票
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue // 跳过自己
		}

		// 并发地发送投票请求
		go func(server int) {
			reply := RequestVoteReply{}
			ok := rf.sendRequestVote(server, &args, &reply)

			if ok {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				// 检查任期是否改变（可能在等待回复时状态已变）
				if rf.state != Candidate || rf.currentTerm != args.Term {
					return
				}

				// 如果对方任期更高，自己变成Follower
				if reply.Term > rf.currentTerm {
					rf.currentTerm = reply.Term
					rf.state = Follower
					rf.votedFor = -1
					return
				}

				// 统计票数
				if reply.VoteGranted {
					rf.votesReceived++
					DPrintf("Server %d received vote from %d, total votes: %d", 
						rf.me, server, rf.votesReceived)
					// 如果获得多数票，成为Leader！
					if rf.votesReceived > len(rf.peers)/2 {
						rf.state = Leader
						// 初始化Leader状态
						rf.initializeLeaderState()
						DPrintf("Server %d becomes Leader in term %d with %d votes", 
							rf.me, rf.currentTerm, rf.votesReceived)
					}
				} else {
					DPrintf("Server %d vote rejected by %d", rf.me, server)
				}
			}
		}(i)
	}
}

// 初始化Leader状态
func (rf *Raft) initializeLeaderState() {
	rf.nextIndex = make([]int, len(rf.peers))
	rf.matchIndex = make([]int, len(rf.peers))
	
	// 乐观假设：所有Follower都有我的所有日志条目  
	lastLogIndex := len(rf.log) - 1
	for i := 0; i < len(rf.peers); i++ {
		rf.nextIndex[i] = lastLogIndex + 1  // 猜测：下次发送的索引
		rf.matchIndex[i] = 0                // 确定：只知道有索引0的虚拟条目
	}
	
	DPrintf("Leader %d initialized: nextIndex=%v, matchIndex=%v", 
		rf.me, rf.nextIndex, rf.matchIndex)
}

// Leader发送日志条目给所有Follower（包括心跳）
func (rf *Raft) sendHeartbeats() {
	rf.mu.Lock()
	// 确保自己还是Leader
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	rf.mu.Unlock()

	// 向每个Follower分别发送个性化的AppendEntries
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue // 不用给自己发
		}

		// 并发发送给每个Follower
		go rf.sendToFollower(i)
	}
}

// 给特定Follower发送AppendEntries
func (rf *Raft) sendToFollower(server int) {
	rf.mu.Lock()
	
	// 再次检查Leader状态
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}

	// 根据nextIndex准备要发送的内容
	nextIndex := rf.nextIndex[server]
	prevLogIndex := nextIndex - 1
	prevLogTerm := 0
	if prevLogIndex > 0 {
		prevLogTerm = rf.log[prevLogIndex].Term
	}

	// 准备要发送的日志条目（从nextIndex开始）
	var entries []LogEntry
	if nextIndex < len(rf.log) {
		entries = rf.log[nextIndex:]
	}

	args := AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderId:     rf.me,
		PrevLogIndex: prevLogIndex,
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	
	rf.mu.Unlock()

	// 发送RPC
	reply := AppendEntriesReply{}
	ok := rf.sendAppendEntries(server, &args, &reply)

	if ok {
		rf.handleAppendEntriesReply(server, &args, &reply)
	}
}

// 处理AppendEntries的回复
func (rf *Raft) handleAppendEntriesReply(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 检查是否还是Leader和相同任期
	if rf.state != Leader || rf.currentTerm != args.Term {
		return
	}

	// 如果Follower告诉我有更高的任期
	if reply.Term > rf.currentTerm {
		rf.currentTerm = reply.Term
		rf.state = Follower
		rf.votedFor = -1
		rf.resetElectionTimeout()
		DPrintf("Leader %d stepping down, found higher term %d", rf.me, reply.Term)
		return
	}

	// 处理AppendEntries的结果
	if reply.Success {
		// 成功：更新nextIndex和matchIndex
		rf.nextIndex[server] = args.PrevLogIndex + len(args.Entries) + 1
		rf.matchIndex[server] = args.PrevLogIndex + len(args.Entries)
		DPrintf("Leader %d: server %d accepted entries up to index %d", 
			rf.me, server, rf.matchIndex[server])
		
		// 检查是否可以提交新的日志条目
		rf.tryCommit()
	} else {
		// 失败：递减nextIndex重试
		if rf.nextIndex[server] > 1 {
			rf.nextIndex[server]--
		}
		DPrintf("Leader %d: server %d rejected, nextIndex now %d", 
			rf.me, server, rf.nextIndex[server])
	}
}

// 尝试提交新的日志条目
func (rf *Raft) tryCommit() {
	// 从commitIndex+1开始，检查每个未提交的日志条目
	for n := rf.commitIndex + 1; n < len(rf.log); n++ {
		// 只能提交当前任期的日志条目
		if rf.log[n].Term != rf.currentTerm {
			continue
		}
		
		// 统计有多少个服务器有这个日志条目
		count := 1 // Leader自己算一个
		for i := 0; i < len(rf.peers); i++ {
			if i != rf.me && rf.matchIndex[i] >= n {
				count++
			}
		}
		
		// 如果大多数服务器都有这个条目，可以提交
		if count > len(rf.peers)/2 {
			rf.commitIndex = n
			DPrintf("Leader %d committed index %d", rf.me, n)
		}
	}
}

func (rf *Raft) ticker() {
	for rf.killed() == false {

		// Your code here (3A)
		// Check if a leader election should be started.

		rf.mu.Lock()
		state := rf.state
		rf.mu.Unlock()

		switch state {
		case Follower:
			// Follower：检查是否需要开始选举
			rf.checkElectionTimeout()

		case Candidate:
			// Candidate：检查是否需要开始新一轮选举
			rf.checkElectionTimeout()

		case Leader:
			// Leader：发送心跳
			rf.sendHeartbeats()
		}

		// pause for a random amount of time between 50 and 350
		// milliseconds.
		// 对于Leader，使用较短的间隔（心跳）
		// 对于Follower和Candidate，使用较长的间隔
		var ms int64
		if state == Leader {
			ms = 50 // Leader每50ms发送一次心跳
		} else {
			ms = 50 + (rand.Int63() % 100) // 其他状态50-150ms检查一次
		}
		time.Sleep(time.Duration(ms) * time.Millisecond)
	}
}

// applier goroutine 应用已提交的日志条目到状态机
func (rf *Raft) applier() {
	for rf.killed() == false {
		rf.mu.Lock()
		
		// 检查是否有需要应用的条目
		if rf.commitIndex > rf.lastApplied {
			// 应用从lastApplied+1到commitIndex的所有条目
			startIdx := rf.lastApplied + 1
			endIdx := rf.commitIndex
			
			// 复制要应用的条目（避免持锁时发送到channel）
			entries := make([]LogEntry, endIdx-startIdx+1)
			for i := startIdx; i <= endIdx; i++ {
				entries[i-startIdx] = rf.log[i]
			}
			
			rf.mu.Unlock()
			
			// 发送ApplyMsg到applyCh
			for i, entry := range entries {
				msg := raftapi.ApplyMsg{
					CommandValid: true,
					Command:      entry.Command,
					CommandIndex: startIdx + i,
				}
				rf.applyCh <- msg
				DPrintf("Server %d applied command at index %d", rf.me, startIdx+i)
			}
			
			// 更新lastApplied
			rf.mu.Lock()
			rf.lastApplied = endIdx
			rf.mu.Unlock()
		} else {
			rf.mu.Unlock()
			// 没有需要应用的条目，休眠一会儿
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
func Make(peers []*labrpc.ClientEnd, me int,
	persister *tester.Persister, applyCh chan raftapi.ApplyMsg) raftapi.Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (3A, 3B, 3C).

	// ==== 初始化持久状态 ====
	rf.currentTerm = 0 // 从第0任期开始
	rf.votedFor = -1   // 还没投票给任何人
	// 初始化日志，索引0放一个虚拟条目
	rf.log = make([]LogEntry, 1)
	rf.log[0] = LogEntry{Command: nil, Term: 0} // 虚拟条目

	// ==== 初始化易失状态 ====
	rf.commitIndex = 0 // 初始没有提交的条目
	rf.lastApplied = 0 // 初始没有应用的条目

	// ==== 初始化Leader状态 (稍后在成为Leader时设置) ====
	rf.nextIndex = nil  // 只有Leader才需要
	rf.matchIndex = nil // 只有Leader才需要

	// ==== 初始化选举状态 ====
	rf.state = Follower // 所有服务器开始都是Follower
	rf.applyCh = applyCh // 保存channel

	// ==== 设置随机的选举超时 ====
	rf.resetElectionTimeout()

	DPrintf("Server %d started as Follower with empty log", me)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()
	
	// start applier goroutine to apply committed entries
	go rf.applier()

	return rf
}
