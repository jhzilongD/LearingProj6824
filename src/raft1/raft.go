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

	// 3A: 领导者选举需要的状态
	currentTerm int   // 当前任期号（初始为0）
	votedFor    int   // 本任期投票给了谁（-1表示还没投票）
	state       State // 当前状态：Follower、Candidate或Leader

	// 用于记录时间
	lastHeartbeat time.Time // 上次收到Leader心跳的时间
	electionTimeout time.Duration // 随机的选举超时时间

	// 用于向上层应用发送已提交的命令
	applyCh chan raftapi.ApplyMsg

	// 选举相关（用于统计投票）
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
	Term        int // 候选人的任期号
	CandidateId int // 候选人的ID（谁在请求投票）
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
type RequestVoteReply struct {
	// Your data here (3A).
	Term        int  // 当前任期号（用于候选人更新自己）
	VoteGranted bool // true表示给候选人投票了
}

// AppendEntries RPC arguments structure (3A只用于心跳)
type AppendEntriesArgs struct {
	Term     int // Leader的任期号
	LeaderId int // Leader的ID，让Follower知道谁是Leader
}

// AppendEntries RPC reply structure
type AppendEntriesReply struct {
	Term    int  // 当前任期号，用于Leader更新自己
	Success bool // 如果Follower接受了AppendEntries，返回true
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
		// 投票！
		rf.votedFor = args.CandidateId
		reply.VoteGranted = true

		// 重置选举超时（因为听到了候选人的声音）
		rf.resetElectionTimeout()
	}
}

// AppendEntries RPC handler (3A只处理心跳)
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 1. 回复自己的任期号
	reply.Term = rf.currentTerm
	reply.Success = false

	// 2. 如果是"前朝余孽"的心跳，拒绝
	if args.Term < rf.currentTerm {
		return
	}

	// 3. 如果是当朝或新朝的Leader
	if args.Term >= rf.currentTerm {
		// 如果是新朝，更新朝代
		if args.Term > rf.currentTerm {
			rf.currentTerm = args.Term
			rf.votedFor = -1 // 新朝代，重置投票
		}
		// 承认Leader，做小弟
		rf.state = Follower
		// 重置选举超时（皇帝还在，不用造反）
		rf.resetElectionTimeout()
		reply.Success = true
	}
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
	index := -1
	term := -1
	isLeader := true

	// Your code here (3B).

	return index, term, isLeader
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

	// 准备请求参数
	args := RequestVoteArgs{
		Term:        rf.currentTerm,
		CandidateId: rf.me,
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
						DPrintf("Server %d becomes Leader in term %d with %d votes", 
							rf.me, rf.currentTerm, rf.votesReceived)
						// TODO: 初始化Leader状态（3B会用到）
					}
				} else {
					DPrintf("Server %d vote rejected by %d", rf.me, server)
				}
			}
		}(i)
	}
}

// Leader发送心跳给所有Follower
func (rf *Raft) sendHeartbeats() {
	rf.mu.Lock()
	// 确保自己还是Leader
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}

	// 准备心跳消息
	args := AppendEntriesArgs{
		Term:     rf.currentTerm,
		LeaderId: rf.me,
	}
	rf.mu.Unlock()

	// 向所有小弟发送"朕还在"的消息
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue // 不用给自己发
		}

		// 并发发送心跳
		go func(server int) {
			reply := AppendEntriesReply{}
			ok := rf.sendAppendEntries(server, &args, &reply)

			if ok {
				rf.mu.Lock()
				defer rf.mu.Unlock()

				// 如果小弟告诉我有新朝代了
				if reply.Term > rf.currentTerm {
					rf.currentTerm = reply.Term
					rf.state = Follower
					rf.votedFor = -1
					rf.resetElectionTimeout()
					DPrintf("Leader %d stepping down, found higher term %d", rf.me, reply.Term)
				}
			}
		}(i)
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

	// 3A: 初始化选举相关的状态
	rf.currentTerm = 0            // 从第0任期开始
	rf.votedFor = -1              // 还没投票给任何人
	rf.state = Follower           // 所有服务器开始都是Follower
	rf.applyCh = applyCh          // 保存channel
	
	// 设置随机的选举超时（这也会初始化lastHeartbeat）
	rf.resetElectionTimeout()
	
	DPrintf("Server %d started as Follower", me)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()

	return rf
}
