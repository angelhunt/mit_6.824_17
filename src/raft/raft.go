package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	"fmt"
	"labrpc"
	"log"
	"math/rand"
	"sync"
	"time"
)

// import "bytes"
// import "encoding/gob"

// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make().
//
type ApplyMsg struct {
	Index       int
	Command     interface{}
	UseSnapshot bool   // ignore for lab2; only used in lab3
	Snapshot    []byte // ignore for lab2; only used in lab3
}

// A Go object implementing a single Raft peer.
//
type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]

	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	term      int
	termRWMU  sync.RWMutex
	state     int
	stateRWMU sync.RWMutex

	//election
	voteFor       int
	voteForRWMU   sync.RWMutex
	voteNumber    int
	electionTimer *time.Timer
	// heartBeatTimer *time.Timer

	vch        chan *RequestVoteArgs
	mostVoteCh chan bool
	ach        chan *AppendEntriesArgs
}

const ( // iota is reset to 0
	Follower = iota
	Candidate
	Leader

	HEART_BEAT_INTERVAL  = 100
	MAX_ELECTION_TIMEOUT = 400
	MIN_ELECTION_TIMEOUT = 300
)

/****************lab2 part1 : (0) helper functions *****************/
func (rf *Raft) getTerm() int {
	rf.termRWMU.RLock()
	defer rf.termRWMU.RUnlock()
	return rf.term
}

func (rf *Raft) setTerm(term int) {
	rf.termRWMU.Lock()
	defer rf.termRWMU.Unlock()
	rf.term = term
}

func (rf *Raft) increaseTerm() {
	rf.termRWMU.Lock()
	defer rf.termRWMU.Unlock()
	rf.term++
}

func (rf *Raft) getState() int {
	rf.stateRWMU.RLock()
	defer rf.stateRWMU.RUnlock()
	return rf.state
}
func (rf *Raft) GetState() (int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	term := rf.getTerm()
	temp := (rf.state == Leader)
	return term, temp
}
func (rf *Raft) isState(state int) bool {
	rf.stateRWMU.RLock()
	defer rf.stateRWMU.RUnlock()
	return state == rf.state
}

func (rf *Raft) setVoteFor(num int) {
	rf.voteForRWMU.Lock()
	defer rf.voteForRWMU.Unlock()
	rf.voteFor = num
}

func (rf *Raft) isVoteFor(index int) bool {
	rf.voteForRWMU.RLock()
	defer rf.voteForRWMU.RUnlock()
	res := rf.voteFor == index
	return res
}

func randomDuration(min int, max int) time.Duration {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	return time.Duration(r.Intn(max-min)+min) * time.Millisecond
}

func randomElectionDuration() time.Duration {
	return randomDuration(MIN_ELECTION_TIMEOUT, MAX_ELECTION_TIMEOUT)
}

func (rf *Raft) resetElectionTimer() {
	rf.electionTimer.Reset(randomElectionDuration())
}

// func (rf *Raft) resetHeartBeatTimer() {
// 	rf.heartBeatTimer.Reset(time.Duration(HEART_BEAT_INTERVAL) * time.Millisecond)
// }

/******************(1)vote related function**********************/

// example RequestVote RPC arguments structure.
// field names must start with capital letters!
type RequestVoteArgs struct {
	// Your data here (2A, 2B).
	Term         int
	CandidateID  int
	LastLogIndex int
	LastLogTerm  int
}

// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	// Your data here (2A).
	Term        int
	VoteGranted bool
	Action      string
}

// example RequestVote RPC handler.
//
//RPC函数,根据args.Term进行分类处理, 成功后使用channel发送消息消息给loop线程
//参考
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	// Your code here (2A, 2B).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	event := fmt.Sprintf("receive vote request form %d server", args.CandidateID)
	if rf.term > args.Term {
		reply.Action = fmt.Sprintf("refuse vote request because request term %d lower than server's term %d", args.Term, rf.term)
		Log(rf, event, reply.Action)
		reply.Term = rf.term
		reply.VoteGranted = false
	} else if rf.term < args.Term {
		reply.Action = fmt.Sprintf("accept vote request because request term %d higher than server's term %d", args.Term, rf.term)
		Log(rf, event, reply.Action)
		rf.term = args.Term
		rf.changeState(Follower)
		rf.voteFor = args.CandidateID
		reply.VoteGranted = true
		// if rf.state == Candidate {
		// 	log.Fatal("Candidate's term lower than arg")
		// }
	} else {
		if rf.voteFor == -1 {
			// if rf.state == Candidate {
			// 	log.Fatal("Candidate's voteFor is -1")
			// }
			reply.Action = fmt.Sprint("accept vote request because server's voteFor equal -1")
			Log(rf, event, reply.Action)
			rf.voteFor = args.CandidateID
			reply.VoteGranted = true
		} else {
			reply.Action = fmt.Sprint("refuse vote request because server's voteFor doesn't equal -1")
			Log(rf, event, reply.Action)
			reply.VoteGranted = false
		}
	}
	if reply.VoteGranted == true { //check me, 第二个条件不确定
		//注意这里一定要用goroutine, 因为go语言中的channel默认是阻塞的
		go func() { rf.vch <- args }()
	}
}

//
// example code to send a RequestVote RPC to a server.
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
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//

//check is term of resquest or response bigger than server's term, modify term to bigger and change state to Follower
//需要外部加锁
func (rf *Raft) checkTerm(term int) bool {
	if term > rf.term {
		rf.term = term
		rf.changeState(Follower)
		return false
	}
	return true
}
func (rf *Raft) syncCheckTerm(term int) bool {
	rf.termRWMU.Lock()
	defer rf.termRWMU.Unlock()
	return rf.checkTerm(term)
}
func (rf *Raft) broadcastVoteRequest() {
	for i := range rf.peers {
		if i == rf.me {
			continue
		}
		if !rf.isState(Candidate) {
			return
		}
		args := RequestVoteArgs{rf.getTerm(), rf.me, 0, 0}
		//第一次检查state, 如果变化了,不在发送请求
		go func(server int, args *RequestVoteArgs) {
			reply := RequestVoteReply{0, false, ""}
			if rf.sendRequestVote(server, args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				//第二次检查,如果之前处理request的goroutine把state改变成了Foller,这里就不在检查reply内容
				// if rf.getTerm() != Candidate {
				// 	return
				// }
				if !rf.checkTerm(reply.Term) {
					Log(rf, "call vote request rpc, vote reply request term less than rf.term", "doesn't increase voteNumber")
				}
				if rf.state != Candidate {
					Log(rf, "call vote request rpc, state doesn't equal Candidate", "doesn't increase voteNumber")
				}
				if reply.VoteGranted == true {
					Log(rf, fmt.Sprintf("get successful reply from %d server", server), "increase voteNumber")
					rf.voteNumber++
					// DPrintf("%d server, Event: requestVote, successfully call vote request rpc in %d server, voteNumber ++\n", rf.me, server)
					//不能放在这来检查,因为可能出现只有一台机器的情况,要保证这种情况下还能运行,所以要在loop中检查是否成为leader
					// if rf.voteNumber > len(rf.peers)/2 {
					// 	rf.changeState(Leader)
					// }
					// if rf.voteNumber > len(rf.peers)>>1 {
					// 	go func() {
					// 		rf.mostVoteCh <- true
					// 	}()
					// }
				}
			} else {
				event := fmt.Sprintf("fail to call vote request RPC at %d server", server)
				Log(rf, event, "retry call RPC")
				// DPrintf("%d server, Event: requestVote, fail to call RequestVote RPC to %d server\n", rf.me, server)
			}
		}(i, &args)
	}
}

//must be lock
func (rf *Raft) syncBeginElection() {
	rf.mu.Lock()
	rf.beginElection()
	rf.mu.Unlock()
}
func (rf *Raft) beginElection() {
	rf.term++
	rf.voteFor = rf.me
	rf.voteNumber = 1
	rf.resetElectionTimer()
	rf.broadcastVoteRequest()
	// rf.increaseTerm()
	// rf.setVoteFor(rf.me)
	// rf.electionTimer.Reset(randomElectionDuration())
	// rf.broadcastVoteRequest()
}

/************************ lab2 part1 (2) append RPC **************************/

//AppendEntries
type AppendEntriesArgs struct {
	Term     int
	LeaderID int
	//preLogIndex int
	//prevLog	Term int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	event := fmt.Sprintf("receive append request from %d server", args.LeaderID)
	if args.Term < rf.term {
		reply.Term = rf.term
		reply.Success = false
		Log(rf, event, fmt.Sprintf("refuse because args Term %d is lower than rf.term %d", args.Term, rf.term))
	} else if args.Term > rf.term {
		reply.Success = true
		rf.term = args.Term
		rf.changeState(Follower)
		Log(rf, event, fmt.Sprintf("accept because args Term %d is bigger than rf.term %d and change state to Follower", args.Term, rf.term))
	} else {
		reply.Success = true
		Log(rf, event, fmt.Sprintf("accept because args Term %d equal rf.term %d", args.Term, rf.term))
	}
	// if reply.Success == true { // 这里是否需要判断还有待商榷
	go func() { rf.ach <- args }()
	// }
}
func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	res := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return res
}

func (rf *Raft) broadcastAppendEntries() {
	for i := 0; i < len(rf.peers); i++ {
		if i == rf.me {
			continue
		}
		args := AppendEntriesArgs{Term: rf.getTerm(), LeaderID: rf.me}
		go func(serverIndex int, args *AppendEntriesArgs) {
			var reply AppendEntriesReply
			if rf.isState(Leader) && rf.sendAppendEntries(serverIndex, args, &reply) {
				rf.mu.Lock()
				defer rf.mu.Unlock()
				rf.checkTerm(reply.Term)
				if rf.checkTerm(reply.Term) && reply.Success {
					go func() {
						rf.ach <- args
					}()
				}
			}
		}(i, &args)
	}
}

/*****************lab2 part1 (3) state function *************************/
func (rf *Raft) syncChangeState(state int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.changeState(state)
}

// return currentTerm and whether this server
// believes it is the leader.
//主要进行状态修改后的初始化工作
//这个函数需要外部加锁,后面使用需要注意
func (rf *Raft) changeState(state int) {
	if state == rf.state {
		return
	}
	rf.state = state
	switch state {
	case Follower:
		rf.setVoteFor(-1)
		rf.resetElectionTimer()
		// rf.resetHeartBeatTimer()
	case Leader:
	case Candidate:
		rf.beginElection()
	default:
		log.Fatal("Unknow state in changeState function parameter")
	}
}

/************************lab2 part 1: (4) init and main logic****************************/
func (rf *Raft) loop() {
	rf.electionTimer = time.NewTimer(randomElectionDuration())
	// rf.heartBeatTimer = time.NewTimer(time.Duration(HEART_BEAT_INTERVAL) * time.Millisecond)
	// debugFormat := "%d server, Event : main loop, current state: %s, event: %s, action: %s"
	var event string

	for {
		switch rf.getState() {
		case Follower:
			select {
			case <-rf.electionTimer.C:
				event = "heartBeat timeout"
				Log(rf, event, "reset voteFor and convert to Candidate")
				rf.syncChangeState(Candidate)
			case vote := <-rf.vch:
				event = "message from vote channel"
				Log(rf, event, "reset timer and check term from vote request reply")
				rf.resetElectionTimer()
				rf.syncCheckTerm(vote.Term)
			case append := <-rf.ach:
				event = "message from heartbeat channel"
				Log(rf, event, "reset heartbeat timer and check term from append request")
				//暂时什么都不做,因为part1只是实现election
				rf.resetElectionTimer()
				// rf.resetHeartBeatTimer()
				rf.syncCheckTerm(append.Term)
			}
		case Candidate:
			//action for vote response is in broadcastVoteRequest functions
			select {
			case appendEntry := <-rf.ach:
				//Rule Candidates, If AppendEntries RPC received from new leader: convert to follower
				//注意这里new的定义,如果发生了网络partition,那么会接收到来自term小于rf.me的append请求,此时需要忽略它
				event = "successfully call append request rpc"
				Log(rf, event, "check term from reply")
				// DPrintf(debugFormat, rf.me, state, event, "check term from append response")
				rf.mu.Lock()
				if appendEntry.Term >= rf.term {
					rf.term = appendEntry.Term
					rf.changeState(Follower)
				}
				rf.mu.Unlock()
			case <-rf.vch:
			// log.Fatalf("!!!!!!!!!!Candidate %d get signal from vch, voteFor:%d", rf.me, rf.voteFor)
			case <-rf.electionTimer.C:
				event = "election time out"
				Log(rf, event, "continue next election")
				rf.syncBeginElection()
			// case <-rf.mostVoteCh:
			// 	event = "get most vote"
			// 	Log(rf, event, "change state to Leader")
			// 	rf.syncChangeState(Leader)
			default:
				rf.mu.Lock()
				if rf.voteNumber > len(rf.peers)/2 {
					event = fmt.Sprintf("get most vote : %d", rf.voteNumber)
					Log(rf, event, "change state to Leader")
					rf.changeState(Leader)
				}
				rf.mu.Unlock()
				// DPrintf(debugFormat, rf.me, state, event, "check voteNumber")
			}
		case Leader:
			Log(rf, "broadcast heartbeat time", "broadcast heartbeat")
			// DPrintf(debugFormat, rf.me, state, event, event)
			rf.broadcastAppendEntries()
			time.Sleep((HEART_BEAT_INTERVAL * 3 / 4) * time.Millisecond)
		}
	}

}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me

	// Your initialization code here (2A, 2B, 2C).
	rf.state = Follower
	rf.voteFor = -1
	rf.vch = make(chan *RequestVoteArgs, 10)
	rf.ach = make(chan *AppendEntriesArgs, 10)
	rf.mostVoteCh = make(chan bool, 10)

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())
	go rf.loop()
	return rf
}

/*************** other ***********/
//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	// Your code here (2C).
	// Example:
	// w := new(bytes.Buffer)
	// e := gob.NewEncoder(w)
	// e.Encode(rf.xxx)
	// e.Encode(rf.yyy)
	// data := w.Bytes()
	// rf.persister.SaveRaftState(data)
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	// Your code here (2C).
	// Example:
	// r := bytes.NewBuffer(data)
	// d := gob.NewDecoder(r)
	// d.Decode(&rf.xxx)
	// d.Decode(&rf.yyy)
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).

	return index, term, isLeader
}

//
// the tester calls Kill() when a Raft instance won't
// be needed again. you are not required to do anything
// in Kill(), but it might be convenient to (for example)
// turn off debug output from this instance.
//
func (rf *Raft) Kill() {
	// Your code here, if desired.
}
