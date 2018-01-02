package raft
// TODO: test6概率性pass...线程同步加锁

import (
    "labrpc"
    "sync"
    "math/rand"
    "time"
)

const (
    LEADER		= 0
    FOLLOWER	= 1
    CANDIDATE	= 2
)

//
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

type LogEntry struct {
    Command interface{}
    Term    int
}

//
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
    currentTerm	int
    votedFor	int
    state		int
    // these three variable should change together (use mu)
    timeout		int
    heartbeat	chan bool

    // append entry
    log			[]LogEntry
    commitIndex int
    lastApplied int
    nextIndex	[]int
    matchIndex	[]int

    // mutex
    logReady    *sync.Cond

    applyCh     chan ApplyMsg
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {

    var term int
    var isleader bool
    // Your code here (2A).
    rf.mu.Lock()
    defer rf.mu.Unlock()
    term = rf.currentTerm
    isleader = false
    if rf.state == LEADER {
        isleader = true
    }
    return term, isleader
}

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

type RequestVoteArgs struct {
    // Your data here (2A, 2B).
    Term		int
    Candidate	int

    LastLogIndex int
    LastLogTerm  int
}

type RequestVoteReply struct {
    // Your data here (2A).
    Term		int
    VoteGranted	bool
}

type AppendEntryArgs struct {
    // Your data here (2A, 2B).
    Term		int
    LeaderID	int
    
    // append entry
    PreLogIndex	 int
    PreLogTerm	 int
    Entries		 []LogEntry
    LeaderCommit int
}

type AppendEntryReply struct {
    // Your data here (2A).
    Term	int
    Success	bool
}

func (rf *Raft) AppendEntry(args *AppendEntryArgs, reply *AppendEntryReply) {
    DPrintf("%d: %d recieve AppendEntry from %d:%d %d\n", rf.currentTerm, rf.me, args.Term, args.LeaderID, len(args.Entries))
    DPrintf("len(rf.log) = %d, args.PreLogIndex = %d\n", len(rf.log), args.PreLogIndex)
    // Your code here (2A, 2B).
    rf.mu.Lock()
    defer rf.mu.Unlock()
    if args.Term < rf.currentTerm {
        reply.Success = false
        reply.Term = rf.currentTerm
        return
    }

    // heart beat
    if args.Entries == nil {
        DPrintf("%d: %d recieve heartbeat from %d\n", rf.currentTerm, rf.me, args.LeaderID)
        old := rf.state
        reply.Term = args.Term
        reply.Success = true
    
        rf.state = FOLLOWER
        
        if args.Term > rf.currentTerm {
            rf.votedFor = -1
            rf.resetTimeout()
            rf.currentTerm = args.Term	
        }

        if args.LeaderCommit > rf.commitIndex {
            // rf的commitIndex只会在这里改变
            rf.commitIndex = args.LeaderCommit
            if len(rf.log) - 1 < rf.commitIndex {
                rf.commitIndex = len(rf.log) - 1
            }
            // apply
            go func() {
                DPrintf("from heartbeat %d, %d apply %d msg\n", args.LeaderID, rf.me, rf.commitIndex - rf.lastApplied)
                for ; rf.lastApplied < rf.commitIndex; {
                    rf.lastApplied++
                    rf.applyCh <- ApplyMsg{
                        rf.lastApplied,
                        rf.log[rf.lastApplied].Command,
                        false,
                        nil,
                    }
                    DPrintf("from heartbeat %d, %d apply msg success: %d\n", args.LeaderID, rf.me, rf.lastApplied)
                }
            }()
        }

        if old == FOLLOWER {
            rf.heartbeat <- true
        }
        return
    }

    if len(rf.log) <= args.PreLogIndex {
        reply.Success = false
        return
    }
    if rf.log[args.PreLogIndex].Term != args.PreLogTerm {
        // delete
        rf.log = rf.log[:args.PreLogIndex]

        reply.Success = false
        return
    }

    for _, v := range args.Entries {
        rf.log = append(rf.log, v)
        continue
    }
    // rf的commitIndex只会在这里改变
    rf.commitIndex = args.LeaderCommit
    if len(rf.log) - 1 < rf.commitIndex {
        rf.commitIndex = len(rf.log) - 1
    }
    
    // apply
    go func() {
        DPrintf("%d apply %d msg\n", rf.me, rf.commitIndex - rf.lastApplied)
        for ; rf.lastApplied < rf.commitIndex; {
            rf.lastApplied++
            DPrintf("lastApplied = %d\n", rf.lastApplied)
            rf.applyCh <- ApplyMsg{
                rf.lastApplied,
                rf.log[rf.lastApplied].Command,
                false,
                nil,
            }
            DPrintf("%d apply msg success: %d\n", rf.me, rf.lastApplied)
        }
    }()
    reply.Success = true
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
    DPrintf("%d: %d recieve RequestVote from %d:%d\n", rf.currentTerm, rf.me, args.Term, args.Candidate)
    // Your code here (2A, 2B).
    rf.mu.Lock()
    defer rf.mu.Unlock()
    if args.Term < rf.currentTerm {
        
        reply.VoteGranted = false
        reply.Term = rf.currentTerm
        DPrintf("%d: %d recieve voteRequest from %d:%d %v\n", rf.currentTerm, rf.me, args.Term, args.Candidate, reply.VoteGranted)
        return
    }

    if args.Term > rf.currentTerm {
        rf.votedFor = -1
        rf.currentTerm = args.Term
    }

    if rf.votedFor == -1 || rf.votedFor == args.Candidate {
        // election restriction
        if args.LastLogTerm < rf.log[len(rf.log) - 1].Term ||
        (args.LastLogTerm == rf.log[len(rf.log) - 1].Term &&
        args.LastLogIndex < len(rf.log) - 1) {
            rf.votedFor = -1
            reply.VoteGranted = false
            DPrintf("%d: %d recieve voteRequest from %d:%d %v\n", rf.currentTerm, rf.me, args.Term, args.Candidate, reply.VoteGranted)
            return
        }

        
        if rf.state == FOLLOWER {
            rf.heartbeat <- true
        }
        rf.state = FOLLOWER
        rf.resetTimeout()
        rf.votedFor = args.Candidate

        
        reply.VoteGranted = true
        reply.Term = args.Term
        DPrintf("%d: %d recieve voteRequest from %d:%d %v\n", rf.currentTerm, rf.me, args.Term, args.Candidate, reply.VoteGranted)
        return
    }
    reply.VoteGranted = false
    reply.Term = args.Term
    DPrintf("%d: %d recieve voteRequest from %d:%d %v\n", rf.currentTerm, rf.me, args.Term, args.Candidate, reply.VoteGranted)
}

func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
    ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
    return ok
}

func (rf *Raft) sendAppendEntry(server int, args *AppendEntryArgs, reply *AppendEntryReply) bool {
    ok := rf.peers[server].Call("Raft.AppendEntry", args, reply)
    return ok
}

func (rf *Raft) Start(command interface{}) (int, int, bool) {
    
    // Your code here (2B).
    if rf.state != LEADER {
        DPrintf("%d get start %v\n", rf.me, false)
        return -1, -1, false
    }

    index := len(rf.log)
    term := rf.currentTerm

    rf.logReady.L.Lock()
    rf.log = append(rf.log, LogEntry{
        command,
        rf.currentTerm,
    })
    rf.logReady.Broadcast()
    rf.logReady.L.Unlock()
    
    DPrintf("%d get start %v\n", rf.me, true)
    DPrintf("command: %v\n", command)
    return index, term, true
}

func (rf *Raft) Kill() {
    // Your code here, if desired.
}

func Make(peers []*labrpc.ClientEnd, me int,
    persister *Persister, applyCh chan ApplyMsg) *Raft {
    rf := &Raft{}
    rf.peers = peers
    rf.persister = persister
    rf.me = me

    // Your initialization code here (2A, 2B, 2C).
    rf.state = FOLLOWER
    rf.currentTerm = 0
    rf.votedFor = -1
    rf.resetTimeout()
    rf.heartbeat = make(chan bool)

    rf.commitIndex = 0
    rf.lastApplied = 0
    rf.matchIndex = make([]int, len(peers))
    rf.nextIndex = make([]int, len(peers))
    rf.log = []LogEntry{LogEntry{nil, -1}}

    rf.logReady = sync.NewCond(&sync.Mutex{})

    rf.applyCh = applyCh

    go func() {
        
        for {
            ticker := time.NewTicker(time.Duration(rf.timeout) * time.Millisecond)
            select {
            case <- rf.heartbeat:
                // do nothing
            case <- ticker.C:
                DPrintf("%d timeout, begin election\n", rf.me)
                // time out, begin leader election
                res := rf.beginElection()
                if res {
                    rf.doLeader()
                }
            }
        }
    }()

    // initialize from state persisted before a crash
    rf.readPersist(persister.ReadRaftState())

    return rf
}

func (rf *Raft) doLeader() {
    DPrintf("%d: %d become leader\n", rf.currentTerm, rf.me)
    // heart beat
    for i := range rf.peers {
        if i == rf.me {
            continue
        }
        
        tmp := i
        go func () {
            t := time.NewTicker(time.Millisecond * time.Duration(150))
            for range t.C {
                rf.mu.Lock()
                if rf.state != LEADER {
                    rf.mu.Unlock()
                    return
                }
                rf.mu.Unlock()
                args := &AppendEntryArgs{rf.currentTerm, rf.me, -1, -1, nil, rf.commitIndex}
                reply := &AppendEntryReply{}
                go func() {
                    ok := rf.sendAppendEntry(tmp, args, reply)
                    if ok && !reply.Success {
                        rf.mu.Lock()
                        rf.state = FOLLOWER
                        rf.resetTimeout()
                        rf.votedFor = -1
                        rf.currentTerm = reply.Term
                        rf.mu.Unlock()
                        t.Stop()
                        return
                    }
                }()
            }
        }()
    }

    // replicate log entry
    for i := range rf.peers {
        if i == rf.me {
            continue
        }
        tmp := i
        go func() {
            for {
                rf.logReady.L.Lock()
                for rf.nextIndex[tmp] >= len(rf.log) {
                    rf.logReady.Wait()
                }
                rf.logReady.L.Unlock()

                toSend := []LogEntry{}
                j := rf.nextIndex[tmp]
                for ; j < len(rf.log); j++ {
                    toSend = append(toSend, rf.log[j])
                }
                args := &AppendEntryArgs{
                    rf.currentTerm,
                    rf.me,
                    rf.nextIndex[tmp] - 1,
                    rf.log[rf.nextIndex[tmp] - 1].Term,
                    toSend,
                    rf.commitIndex,
                }
                reply := &AppendEntryReply{}

                rf.mu.Lock()
                if rf.state != LEADER {
                    rf.mu.Unlock()
                    return
                }
                rf.mu.Unlock()

                ok := rf.sendAppendEntry(tmp, args, reply)
                if ok {
                    if reply.Success {
                        DPrintf("%d send log entry to %d success! j = %d, len(rd.log) = %d\n", rf.me, tmp, j, len(rf.log))
                        rf.nextIndex[tmp] = j
                    } else {
                        // decrement nextIndex and retry
                        DPrintf("%d send log entry to %d failed!\n", rf.me, tmp)
                        if rf.nextIndex[tmp] > 0 {
                            rf.nextIndex[tmp]--
                        }
                    }
                }
            }
        }()
    }

    // apply command
    // Leader只要实现半数replicate即可apply
    go func() {
        count := 0
        for {
            rf.mu.Lock()
            if rf.state != LEADER {
                rf.mu.Unlock()
                return
            }
            rf.mu.Unlock()

            for i := range rf.peers {
                if i == rf.me {
                    continue
                }
                if rf.nextIndex[i] - 1 >= rf.commitIndex + 1 {
                    count++
                }
            }
            if count >= len(rf.peers) / 2 {
                // apply msg
                // rf.commitIndex 只会在这里改变
                rf.commitIndex++
                DPrintf("%d apply %d msg\n", rf.me, rf.commitIndex - rf.lastApplied)
                DPrintf("lastApplied = %d, commitIndex=%d\n",rf.lastApplied, rf.commitIndex)
                for ; rf.lastApplied < rf.commitIndex; {
                    rf.lastApplied++
                    go func() {
                        rf.applyCh <- ApplyMsg{
                            rf.lastApplied,
                            rf.log[rf.lastApplied].Command,
                            false,
                            nil,
                        }
                        DPrintf("%d apply msg success: %d\n", rf.me, rf.lastApplied)
                    }()
                }
            }
            count = 0
            time.Sleep(10 * time.Millisecond)
        }
    }()

    // no longer leader, exit
    for {
        rf.mu.Lock()
        if rf.state != LEADER {
            rf.mu.Unlock()
            break	
        }
        rf.mu.Unlock()
    }
    DPrintf("================%d: %d not leader\n", rf.currentTerm, rf.me)
}

func (rf *Raft) beginElection() bool {
    rf.mu.Lock()
    defer rf.mu.Unlock()
    count := 1
    voted := 1
    m := sync.Mutex{}
    // vote itself
    rf.votedFor = rf.me
    rf.state = CANDIDATE
    rf.currentTerm++
    // vote request
    for i := range rf.peers {
        if i == rf.me {
            continue
        }
        
        tmp := i
        go func() {
            ok := false
            args := RequestVoteArgs{rf.currentTerm, rf.me, len(rf.log) - 1, rf.log[len(rf.log) - 1].Term}
            reply := RequestVoteReply{}
            for ok == false {
                ok = rf.sendRequestVote(tmp, &args, &reply)
            }
            if reply.VoteGranted {
                m.Lock()
                voted++
                count++
                m.Unlock()
                
            } else {
                rf.currentTerm = reply.Term
                rf.state = FOLLOWER
                DPrintf("%d:%d from leader become follower\n", rf.currentTerm, rf.me)
                m.Lock()
                count++
                m.Unlock()
                
            }
        }()
    }
    stop := false
    go func() {
        t := time.NewTicker(time.Duration(rf.timeout * 10) * time.Millisecond)
        <- t.C
        stop = true
    }()
    for {
        if stop {
            m.Lock()
            rf.state = FOLLOWER
            rf.votedFor = -1
            rf.resetTimeout()
            m.Unlock()
            return false
        }
        m.Lock()
        if voted > len(rf.peers) / 2 {
            m.Unlock()
            rf.state = LEADER

            // Reinitialized nextIndex and matchIndex
            for i := range rf.nextIndex {
                rf.nextIndex[i] = len(rf.log)
                rf.matchIndex[i] = 0
            }
            return true
        }
        if count == len(rf.peers) {
            m.Unlock()
            rf.state = FOLLOWER
            return false
        }
        m.Unlock()
    }
}

func (rf *Raft) resetTimeout() {
    n := rand.Intn(250)
    rf.timeout = n + 300
}
