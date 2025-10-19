# MIT 6.824 分布式系统 Labs 完整教程

> 本教程详细讲解MIT 6.824课程的5个实验，从MapReduce到分片KV数据库，逐步构建一个完整的分布式系统。

## 目录

- [Lab 1: MapReduce](#lab-1-mapreduce)
- [Lab 2: Key/Value Server](#lab-2-keyvalue-server)
- [Lab 3: Raft](#lab-3-raft)
- [Lab 4: Fault-tolerant Key/Value Service](#lab-4-fault-tolerant-keyvalue-service)
- [Lab 5: Sharded Key/Value Service](#lab-5-sharded-keyvalue-service)

---

# Lab 1: MapReduce

## 1.1 实验概述

MapReduce是Google提出的分布式计算框架，本实验要求实现一个简化版MapReduce系统，包括Coordinator（Master）和Worker两个组件。

### 学习目标
- 理解MapReduce编程模型
- 掌握分布式任务调度
- 实现基本的容错机制
- 熟悉Go语言RPC编程

### 实验架构
```
┌──────────────┐
│  Coordinator │ ←── 任务调度中心
└──────┬───────┘
       │ RPC
   ┌───┴────┐
   │        │
┌──▼──┐  ┌──▼──┐
│Worker│  │Worker│ ←── 任务执行节点
└──────┘  └──────┘
```

## 1.2 MapReduce原理

### 执行流程

1. **输入分片**：将输入数据分成M个分片，每个分片对应一个Map任务
2. **Map阶段**：并行处理每个分片，产生中间键值对
3. **Shuffle阶段**：按键的hash值将中间数据分配到R个Reduce任务
4. **Reduce阶段**：对每个键的所有值进行聚合操作
5. **输出结果**：生成最终输出文件

### 数据流示例（Word Count）

```
输入文件: "hello world hello"
    ↓ Map
中间数据: [(hello,1), (world,1), (hello,1)]
    ↓ Shuffle & Sort
分组数据: [(hello,[1,1]), (world,[1])]
    ↓ Reduce
输出结果: [(hello,2), (world,1)]
```

## 1.3 核心数据结构

### RPC通信协议 (`rpc.go`)

```go
// 任务类型定义
type TaskType int
const (
    MapTask    TaskType = iota  // Map任务
    ReduceTask                   // Reduce任务  
    WaitTask                     // 等待（无可用任务）
    ExitTask                     // 退出（所有任务完成）
)

// 任务信息
type Task struct {
    TaskType   TaskType
    TaskId     int      // 任务唯一标识
    InputFile  string   // Map任务的输入文件
    NReduce    int      // Reduce任务总数（用于分桶）
    NMap       int      // Map任务总数
}

// RPC请求/响应结构
type GetTaskArgs struct {}
type GetTaskReply struct {
    Task Task
}

type ReportTaskArgs struct {
    TaskType TaskType
    TaskId   int
    Success  bool  // 任务是否成功
}
type ReportTaskReply struct {}
```

### 任务状态管理 (`coordinator.go`)

```go
// 任务状态
type TaskState int
const (
    TaskIdle       TaskState = iota  // 空闲
    TaskInProgress                    // 进行中
    TaskCompleted                     // 已完成
)

// 任务信息
type TaskInfo struct {
    TaskId    int
    State     TaskState
    StartTime time.Time  // 用于超时检测
    InputFile string     // Map任务的输入文件
}

// Coordinator结构
type Coordinator struct {
    mu          sync.Mutex     // 保护共享状态
    files       []string       // 输入文件列表
    nReduce     int           // Reduce任务数
    nMap        int           // Map任务数
    
    mapTasks    []TaskInfo    // Map任务状态
    mapDone     int           // 已完成的Map任务数
    
    reduceTasks []TaskInfo    // Reduce任务状态
    reduceDone  int           // 已完成的Reduce任务数
    
    phase       TaskType      // 当前阶段
    allDone     bool         // 所有任务是否完成
}
```

## 1.4 实现详解

### Coordinator实现

#### 1. 初始化 (`MakeCoordinator`)

```go
func MakeCoordinator(files []string, nReduce int) *Coordinator {
    c := Coordinator{
        files:       files,
        nReduce:     nReduce,
        nMap:        len(files),
        phase:       MapTask,
        allDone:     false,
        mapTasks:    make([]TaskInfo, len(files)),
        reduceTasks: make([]TaskInfo, nReduce),
    }
    
    // 初始化Map任务
    for i := 0; i < c.nMap; i++ {
        c.mapTasks[i] = TaskInfo{
            TaskId:    i,
            State:     TaskIdle,
            InputFile: files[i],
        }
    }
    
    // 初始化Reduce任务
    for i := 0; i < c.nReduce; i++ {
        c.reduceTasks[i] = TaskInfo{
            TaskId: i,
            State:  TaskIdle,
        }
    }
    
    // 启动超时检测
    go c.checkTimeoutTasks()
    
    // 启动RPC服务
    c.server()
    return &c
}
```

#### 2. 任务分配 (`GetTask`)

```go
func (c *Coordinator) GetTask(args *GetTaskArgs, reply *GetTaskReply) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    // 检查是否全部完成
    if c.allDone {
        reply.Task.TaskType = ExitTask
        return nil
    }
    
    // Map阶段：分配Map任务
    if c.phase == MapTask {
        for i := 0; i < c.nMap; i++ {
            if c.mapTasks[i].State == TaskIdle {
                // 更新任务状态
                c.mapTasks[i].State = TaskInProgress
                c.mapTasks[i].StartTime = time.Now()
                
                // 构造任务信息
                reply.Task = Task{
                    TaskType:  MapTask,
                    TaskId:    i,
                    InputFile: c.files[i],
                    NReduce:   c.nReduce,
                    NMap:      c.nMap,
                }
                return nil
            }
        }
        // 无可用Map任务，等待
        reply.Task.TaskType = WaitTask
        return nil
    }
    
    // Reduce阶段：分配Reduce任务
    for i := 0; i < c.nReduce; i++ {
        if c.reduceTasks[i].State == TaskIdle {
            c.reduceTasks[i].State = TaskInProgress
            c.reduceTasks[i].StartTime = time.Now()
            
            reply.Task = Task{
                TaskType: ReduceTask,
                TaskId:   i,
                NReduce:  c.nReduce,
                NMap:     c.nMap,
            }
            return nil
        }
    }
    
    reply.Task.TaskType = WaitTask
    return nil
}
```

#### 3. 任务完成报告 (`ReportTask`)

```go
func (c *Coordinator) ReportTask(args *ReportTaskArgs, reply *ReportTaskReply) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    if args.TaskType == MapTask {
        // 验证任务ID有效性
        if args.TaskId < 0 || args.TaskId >= c.nMap {
            return nil
        }
        
        // 避免重复完成
        if c.mapTasks[args.TaskId].State == TaskCompleted {
            return nil
        }
        
        if args.Success {
            c.mapTasks[args.TaskId].State = TaskCompleted
            c.mapDone++
            
            // 所有Map完成，进入Reduce阶段
            if c.mapDone == c.nMap {
                c.phase = ReduceTask
            }
        } else {
            // 任务失败，重置为空闲
            c.mapTasks[args.TaskId].State = TaskIdle
        }
    } else if args.TaskType == ReduceTask {
        // Reduce任务处理逻辑类似
        if args.TaskId < 0 || args.TaskId >= c.nReduce {
            return nil
        }
        
        if c.reduceTasks[args.TaskId].State == TaskCompleted {
            return nil
        }
        
        if args.Success {
            c.reduceTasks[args.TaskId].State = TaskCompleted
            c.reduceDone++
            
            // 所有任务完成
            if c.reduceDone == c.nReduce {
                c.allDone = true
            }
        } else {
            c.reduceTasks[args.TaskId].State = TaskIdle
        }
    }
    
    return nil
}
```

#### 4. 超时检测机制

```go
func (c *Coordinator) checkTimeoutTasks() {
    for {
        time.Sleep(time.Second)
        
        c.mu.Lock()
        
        // 检查Map任务超时
        for i := 0; i < c.nMap; i++ {
            if c.mapTasks[i].State == TaskInProgress {
                if time.Since(c.mapTasks[i].StartTime) > 10*time.Second {
                    log.Printf("Map task %d timeout, reset to idle", i)
                    c.mapTasks[i].State = TaskIdle
                }
            }
        }
        
        // 检查Reduce任务超时
        for i := 0; i < c.nReduce; i++ {
            if c.reduceTasks[i].State == TaskInProgress {
                if time.Since(c.reduceTasks[i].StartTime) > 10*time.Second {
                    log.Printf("Reduce task %d timeout, reset to idle", i)
                    c.reduceTasks[i].State = TaskIdle
                }
            }
        }
        
        c.mu.Unlock()
        
        if c.Done() {
            return
        }
    }
}
```

### Worker实现

#### 1. 主循环 (`Worker`)

```go
func Worker(mapf func(string, string) []KeyValue,
           reducef func(string, []string) string) {
    
    for {
        // 请求任务
        task := getTask()
        
        // 根据任务类型执行
        switch task.TaskType {
        case MapTask:
            performMapTask(mapf, &task)
        case ReduceTask:
            performReduceTask(reducef, &task)
        case WaitTask:
            time.Sleep(time.Second)
        case ExitTask:
            return
        }
    }
}
```

#### 2. Map任务执行

```go
func performMapTask(mapf func(string, string) []KeyValue, task *Task) {
    // 1. 读取输入文件
    content, err := ioutil.ReadFile(task.InputFile)
    if err != nil {
        reportTask(task.TaskType, task.TaskId, false)
        return
    }
    
    // 2. 调用用户Map函数
    kva := mapf(task.InputFile, string(content))
    
    // 3. 按hash分桶
    buckets := make([][]KeyValue, task.NReduce)
    for _, kv := range kva {
        bucket := ihash(kv.Key) % task.NReduce
        buckets[bucket] = append(buckets[bucket], kv)
    }
    
    // 4. 写入中间文件
    for i := 0; i < task.NReduce; i++ {
        filename := fmt.Sprintf("mr-%d-%d", task.TaskId, i)
        file, _ := os.Create(filename)
        encoder := json.NewEncoder(file)
        
        for _, kv := range buckets[i] {
            encoder.Encode(&kv)
        }
        file.Close()
    }
    
    // 5. 报告完成
    reportTask(task.TaskType, task.TaskId, true)
}
```

#### 3. Reduce任务执行

```go
func performReduceTask(reducef func(string, []string) string, task *Task) {
    // 1. 读取所有相关中间文件
    kva := []KeyValue{}
    for mapId := 0; mapId < task.NMap; mapId++ {
        filename := fmt.Sprintf("mr-%d-%d", mapId, task.TaskId)
        file, err := os.Open(filename)
        if err != nil {
            continue  // 文件可能不存在
        }
        
        decoder := json.NewDecoder(file)
        for {
            var kv KeyValue
            if err := decoder.Decode(&kv); err != nil {
                break
            }
            kva = append(kva, kv)
        }
        file.Close()
    }
    
    // 2. 按键排序
    sort.Slice(kva, func(i, j int) bool {
        return kva[i].Key < kva[j].Key
    })
    
    // 3. 对每个键调用Reduce
    outputFile, _ := os.Create(fmt.Sprintf("mr-out-%d", task.TaskId))
    
    i := 0
    for i < len(kva) {
        // 找出相同键的范围
        j := i + 1
        for j < len(kva) && kva[j].Key == kva[i].Key {
            j++
        }
        
        // 收集值
        values := []string{}
        for k := i; k < j; k++ {
            values = append(values, kva[k].Value)
        }
        
        // 调用Reduce函数
        output := reducef(kva[i].Key, values)
        fmt.Fprintf(outputFile, "%v %v\n", kva[i].Key, output)
        
        i = j
    }
    outputFile.Close()
    
    // 4. 报告完成
    reportTask(task.TaskType, task.TaskId, true)
}
```

### 应用示例：Word Count (`mrapps/wc.go`)

```go
// Map函数：将文本分割成单词，每个单词输出(word, "1")
func Map(filename string, contents string) []mr.KeyValue {
    ff := func(r rune) bool { return !unicode.IsLetter(r) }
    words := strings.FieldsFunc(contents, ff)
    
    kva := []mr.KeyValue{}
    for _, w := range words {
        kv := mr.KeyValue{w, "1"}
        kva = append(kva, kv)
    }
    return kva
}

// Reduce函数：计算每个单词出现的次数
func Reduce(key string, values []string) string {
    return strconv.Itoa(len(values))
}
```

## 1.5 关键设计决策

### 1. 中间文件命名规则

使用`mr-X-Y`格式（X=Map任务ID，Y=Reduce任务ID）

**原因**：
- 明确的对应关系：Reduce任务知道读取哪些文件
- 避免命名冲突：任务ID保证唯一性
- 便于调试：可以直接查看中间结果

### 2. Hash分桶策略

使用`ihash(key) % nReduce`决定键的去向

**目的**：
- 保证相同键到同一Reduce：这是MapReduce语义要求
- 负载均衡：hash函数保证均匀分布
- 确定性：相同输入产生相同输出

### 3. 超时重试机制

10秒超时后将任务重置为空闲状态

**考虑因素**：
- 容错性：处理Worker崩溃或网络故障
- 性能：避免等待时间过长
- 简单性：不需要心跳机制

## 1.6 测试与调试

### 运行测试

```bash
# 进入主目录
cd ~/6.5840/src/main

# 运行所有测试
bash test-mr.sh

# 运行特定测试
bash test-mr.sh wc        # Word Count基础测试
bash test-mr.sh indexer    # 倒排索引测试
bash test-mr.sh map-parallelism    # Map并行测试
bash test-mr.sh reduce-parallelism # Reduce并行测试
bash test-mr.sh crash      # 容错测试
```


## 1.7 性能优化思考

### 1. 减少锁竞争
```go
// 不好：持有锁时间过长
c.mu.Lock()
// 大量计算...
c.mu.Unlock()

// 好：最小化临界区
c.mu.Lock()
taskCopy := c.mapTasks[i]  // 复制需要的数据
c.mu.Unlock()
// 在锁外进行计算
```


### 与真实系统的对比

| 特性 | Lab1实现 | 真实MapReduce |
|-----|---------|--------------|
| 调度 | 简单轮询 | 考虑数据本地性 |
| 容错 | 超时重试 | 心跳+备份任务 |
| 存储 | 本地文件 | 分布式文件系统 |
| 规模 | 几个Worker | 数千个节点 |

---

# Lab 2: Key/Value Server

## 2.1 实验概述

Lab2实现一个版本化的键值存储服务，分为两个部分：
- **Part 2.1**: 版本化KV服务器（Get/Put操作）
- **Part 2.2**: 基于KV的分布式锁

### 学习目标
- 理解客户端-服务器架构
- 掌握版本化并发控制
- 实现分布式系统的重试语义
- 用存储原语构建同步机制

### 实验架构
```
Lab2.1: 版本化KV服务      Lab2.2: 分布式锁
┌─────────────────┐       ┌─────────────────┐
│   KV Client     │       │     Lock        │
│   (Get/Put)     │◄──────┤   (Acquire/     │
└─────────────────┘       │    Release)     │
         │                └─────────────────┘
         │ RPC                      │
         ▼                          │
┌─────────────────┐                 │
│   KV Server     │◄────────────────┘
│  - data map     │
│  - versions map │
└─────────────────┘
```

## 2.2 Part 2.1: 版本化键值服务

### 核心思想

这不是普通的KV存储，而是**版本化**的KV存储：
- 每个key都有一个版本号（`Tversion`）
- 只能在版本匹配时更新值
- 解决并发更新的ABA问题

### 数据结构设计

```go
type KVServer struct {
    mu       sync.Mutex              // 保护并发访问
    data     map[string]string       // key -> value 映射
    versions map[string]rpc.Tversion // key -> version 映射
}

type Tversion uint64  // 版本号类型

// RPC消息结构
type GetArgs struct {
    Key string
}

type GetReply struct {
    Value   string
    Version Tversion
    Err     Err        // OK, ErrNoKey
}

type PutArgs struct {
    Key     string
    Value   string
    Version Tversion   // 期望的当前版本
}

type PutReply struct {
    Err Err           // OK, ErrNoKey, ErrVersion
}
```

### Server端实现

#### Get操作
```go
func (kv *KVServer) Get(args *GetArgs, reply *GetReply) {
    kv.mu.Lock()
    defer kv.mu.Unlock()
    
    value, exists := kv.data[args.Key]
    if !exists {
        reply.Err = rpc.ErrNoKey
        return
    }
    
    reply.Value = value
    reply.Version = kv.versions[args.Key]
    reply.Err = rpc.OK
}
```

#### Put操作（版本化更新）
```go
func (kv *KVServer) Put(args *PutArgs, reply *PutReply) {
    kv.mu.Lock()
    defer kv.mu.Unlock()
    
    currentVersion, exists := kv.versions[args.Key]
    
    if exists {
        // key存在，检查版本匹配
        if args.Version != currentVersion {
            reply.Err = rpc.ErrVersion  // 版本不匹配
            return
        }
        // 更新值并递增版本
        kv.data[args.Key] = args.Value
        kv.versions[args.Key] = currentVersion + 1
        reply.Err = rpc.OK
    } else {
        // key不存在
        if args.Version == 0 {
            // 版本0表示创建新key
            kv.data[args.Key] = args.Value
            kv.versions[args.Key] = 1
            reply.Err = rpc.OK
        } else {
            reply.Err = rpc.ErrNoKey
        }
    }
}
```

### Client端智能重试

#### Get操作（幂等重试）
```go
func (ck *Clerk) Get(key string) (string, rpc.Tversion, rpc.Err) {
    args := &rpc.GetArgs{Key: key}
    reply := &rpc.GetReply{}
    
    // Get是幂等的，可以无限重试
    for {
        ok := ck.clnt.Call(ck.server, "KVServer.Get", args, reply)
        if ok {
            return reply.Value, reply.Version, reply.Err
        }
        // 网络失败，继续重试
    }
}
```

#### Put操作（处理不确定性）
```go
func (ck *Clerk) Put(key, value string, version rpc.Tversion) rpc.Err {
    args := &rpc.PutArgs{Key: key, Value: value, Version: version}
    reply := &rpc.PutReply{}
    
    firstAttempt := true
    
    for {
        ok := ck.clnt.Call(ck.server, "KVServer.Put", args, reply)
        if ok {
            switch reply.Err {
            case rpc.OK:
                return rpc.OK
            case rpc.ErrVersion:
                if firstAttempt {
                    return rpc.ErrVersion  // 确定失败
                } else {
                    return rpc.ErrMaybe    // 不确定！
                }
            default:
                return reply.Err
            }
        }
        firstAttempt = false  // 标记后续为重试
    }
}
```

### 关键设计：ErrMaybe语义

**为什么需要ErrMaybe？**

分布式系统中的经典问题：
```
场景：Client发送Put请求，但响应丢失

第一次Put → 网络超时 → Client重试Put
但Server可能已经处理了第一个请求！

解决方案：
- 第一次失败：确定操作没执行 → ErrVersion  
- 重试时失败：可能已执行（响应丢失）→ ErrMaybe
```

这保证了语义的准确性：
- `ErrVersion`: 确定操作未执行
- `ErrMaybe`: 不确定操作是否执行（需要应用层处理）

## 2.3 Part 2.2: 基于KV的分布式锁

### 设计思路

巧妙地使用版本化KV存储实现分布式锁：
- 锁状态存储在KV中
- 利用版本机制保证原子性
- 支持锁的重入

### 数据结构
```go
type Lock struct {
    ck       kvtest.IKVClerk  // KV客户端
    key      string           // 锁在KV中的key
    clientID string           // 锁持有者的唯一ID
    version  rpc.Tversion     // 持有锁时的版本
}

func MakeLock(ck kvtest.IKVClerk, lockKey string) *Lock {
    lk := &Lock{
        ck:       ck,
        key:      lockKey,
        clientID: kvtest.RandValue(8), // 生成唯一ID
    }
    // 初始化锁状态
    lk.ck.Put(lockKey, "", 0)  // 空值表示未锁定
    return lk
}
```

### 锁状态编码

巧妙的状态表示：
```go
锁状态编码：
- key不存在     → 锁未初始化
- value=""      → 锁存在但未被持有  
- value=clientID → 锁被clientID持有
```

### Acquire实现
```go
func (lk *Lock) Acquire() {
    for {
        value, version, err := lk.ck.Get(lk.key)
        
        switch err {
        case rpc.ErrNoKey:
            // 锁不存在，尝试创建并获取
            if lk.ck.Put(lk.key, lk.clientID, 0) == rpc.OK {
                lk.version = 1
                return  // 成功获取锁
            }
            // 创建失败，说明有人抢先创建，继续重试
            
        case rpc.OK:
            switch value {
            case "":
                // 锁未被持有，尝试获取
                if lk.ck.Put(lk.key, lk.clientID, version) == rpc.OK {
                    lk.version = version + 1
                    return  // 成功获取锁
                }
                // 获取失败（版本冲突），继续重试
                
            case lk.clientID:
                // 我已经持有锁（重入锁支持）
                lk.version = version
                return
                
            default:
                // 锁被其他客户端持有，继续等待
            }
        }
        // 继续重试
    }
}
```

### Release实现
```go
func (lk *Lock) Release() {
    // 使用记录的版本号释放锁
    lk.ck.Put(lk.key, "", lk.version)
    
    // 注意：不检查错误，因为：
    // - 成功(OK)：锁被释放
    // - 失败(ErrVersion)：锁已被他人获取（可能超时）
    // - 失败(ErrMaybe)：不确定，但可接受
}
```

## 2.4 关键设计决策分析

### 1. 版本化并发控制

**解决ABA问题**：
```
时间线示例：
T1: Client1读取 key="x", value="1", version=5
T2: Client2修改 key="x" → "2" (version=6)  
T3: Client3修改 key="x" → "1" (version=7)  ← 值恢复！
T4: Client1尝试更新，没有版本检查会成功，但这是错误的

有了版本检查：
T4: Client1使用version=5更新失败（当前是version=7）
```

版本机制确保了**线性一致性**。

### 2. 分布式锁的原子性

利用KV的版本化Put实现锁操作的原子性：
```go
// 原子的"compare-and-swap"语义
success := kv.Put(lockKey, myID, expectedVersion)
if success {
    // 原子地获取了锁
}
```

## 2.5 测试与调试

### 运行测试
```bash
cd src/kvsrv1
go test -v  # KV服务测试

cd lock
go test -v  # 分布式锁测试
```







## 2.8 为Lab3做准备

Lab2为后续的Raft奠定基础：

1. **RPC重试机制** → Raft中的网络处理
2. **版本化状态** → Raft中的term概念  
3. **强一致性** → Raft提供的一致性保证
4. **分布式锁** → 理解分布式同步的复杂性

Lab2教会了你在不可靠网络上构建可靠服务的基本技能！

---

# Lab 3: Raft

## 3.1 实验概述

实现Raft共识算法，分为四个部分：
- **Part 3A**: 领导者选举
- **Part 3B**: 日志复制
- **Part 3C**: 持久化
- **Part 3D**: 日志压缩

### 学习目标
- 深入理解Raft算法
- 实现分布式共识
- 处理网络分区和节点故障
- 优化性能和正确性

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

 

# Lab 4: Fault-tolerant Key/Value Service

## 4.1 实验概述

基于Raft构建容错的键值服务，将Lab2和Lab3结合。

### 学习目标
- 集成Raft到应用层
- 实现线性一致性读写
- 处理配置变更
- 优化客户端重试逻辑

*（Lab 4详细内容待补充）*

---

# Lab 5: Sharded Key/Value Service

## 5.1 实验概述

实现分片的键值服务，支持动态负载均衡和重配置。

### 学习目标
- 实现数据分片
- 处理分片迁移
- 配置管理服务
- 保证迁移过程的一致性

*（Lab 5详细内容待补充）*

---

## 总结

通过这5个实验，你将：
1. **Lab1**: 掌握分布式计算的基础 - MapReduce
2. **Lab2**: 实现基本的客户端-服务器系统
3. **Lab3**: 深入理解分布式共识 - Raft
4. **Lab4**: 构建容错的分布式服务
5. **Lab5**: 实现生产级的分片存储系统

每个实验都建立在前一个的基础上，最终构建出一个完整的分布式存储系统。

## 参考资源

- [课程主页](https://pdos.csail.mit.edu/6.824/)
- [MapReduce论文](https://pdos.csail.mit.edu/6.824/papers/mapreduce.pdf)
- [Raft论文](https://pdos.csail.mit.edu/6.824/papers/raft-extended.pdf)
- [Raft可视化](https://raft.github.io/)
- [Go语言官方文档](https://golang.org/doc/)

## 学习建议

1. **循序渐进**: 按照Lab顺序完成，不要跳跃
2. **深入理解**: 先读论文，理解算法原理
3. **充分测试**: 使用提供的测试用例，确保正确性
4. **性能优化**: 在正确的基础上再考虑优化
5. **交流讨论**: 参与课程论坛，分享经验

祝你在分布式系统的学习之旅中收获满满！