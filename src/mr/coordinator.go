package mr

import "log"
import "net"
import "os"
import "net/rpc"
import "net/http"
import "sync"
import "time"


// Task state
type TaskState int

const (
	TaskIdle TaskState = iota
	TaskInProgress
	TaskCompleted
)

type TaskInfo struct {
	TaskId    int
	State     TaskState
	StartTime time.Time
	InputFile string
}

type Coordinator struct {
	// 同步锁
	mu sync.Mutex
	
	// 输入文件和配置
	files   []string // 输入文件列表
	nReduce int      // Reduce任务数量
	nMap    int      // Map任务数量
	
	// Map任务状态跟踪
	mapTasks    []TaskInfo
	mapDone     int // 已完成的Map任务数量
	
	// Reduce任务状态跟踪
	reduceTasks []TaskInfo  
	reduceDone  int // 已完成的Reduce任务数量
	
	// 阶段控制
	phase     TaskType // 当前阶段：MapTask 或 ReduceTask
	allDone   bool     // 所有任务是否完成
}

// RPC handler for workers to get tasks
func (c *Coordinator) GetTask(args *GetTaskArgs, reply *GetTaskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查是否所有任务都完成
	if c.allDone {
		reply.Task.TaskType = ExitTask
		return nil
	}

	// 根据当前阶段分配任务
	if c.phase == MapTask {
		// 分配Map任务
		for i := 0; i < c.nMap; i++ {
			if c.mapTasks[i].State == TaskIdle {
				// 分配这个Map任务
				c.mapTasks[i].State = TaskInProgress
				c.mapTasks[i].StartTime = time.Now()
				
				reply.Task.TaskType = MapTask
				reply.Task.TaskId = i
				reply.Task.InputFile = c.files[i]
				reply.Task.NReduce = c.nReduce
				reply.Task.NMap = c.nMap
				return nil
			}
		}
		// 没有可用的Map任务，让worker等待
		reply.Task.TaskType = WaitTask
		return nil
	} else {
		// 分配Reduce任务
		for i := 0; i < c.nReduce; i++ {
			if c.reduceTasks[i].State == TaskIdle {
				// 分配这个Reduce任务
				c.reduceTasks[i].State = TaskInProgress
				c.reduceTasks[i].StartTime = time.Now()
				
				reply.Task.TaskType = ReduceTask
				reply.Task.TaskId = i
				reply.Task.NReduce = c.nReduce
				reply.Task.NMap = c.nMap
				return nil
			}
		}
		// 没有可用的Reduce任务，让worker等待
		reply.Task.TaskType = WaitTask
		return nil
	}
}

// RPC handler for workers to report task completion
func (c *Coordinator) ReportTask(args *ReportTaskArgs, reply *ReportTaskReply) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// 检查任务是否已经完成（避免重复完成）
	if args.TaskType == MapTask {
		if args.TaskId < 0 || args.TaskId >= c.nMap {
			return nil // 无效的任务ID
		}
		if c.mapTasks[args.TaskId].State == TaskCompleted {
			return nil // 任务已完成
		}
		
		if args.Success {
			c.mapTasks[args.TaskId].State = TaskCompleted
			c.mapDone++
			
			// 检查是否所有Map任务都完成
			if c.mapDone == c.nMap {
				c.phase = ReduceTask // 进入Reduce阶段
			}
		} else {
			// 任务失败，重新设为空闲状态
			c.mapTasks[args.TaskId].State = TaskIdle
		}
	} else if args.TaskType == ReduceTask {
		if args.TaskId < 0 || args.TaskId >= c.nReduce {
			return nil // 无效的任务ID
		}
		if c.reduceTasks[args.TaskId].State == TaskCompleted {
			return nil // 任务已完成
		}
		
		if args.Success {
			c.reduceTasks[args.TaskId].State = TaskCompleted
			c.reduceDone++
			
			// 检查是否所有Reduce任务都完成
			if c.reduceDone == c.nReduce {
				c.allDone = true // 所有任务完成
			}
		} else {
			// 任务失败，重新设为空闲状态
			c.reduceTasks[args.TaskId].State = TaskIdle
		}
	}

	return nil
}

//
// an example RPC handler.
//
// the RPC argument and reply types are defined in rpc.go.
//
func (c *Coordinator) Example(args *ExampleArgs, reply *ExampleReply) error {
	reply.Y = args.X + 1
	return nil
}


//
// start a thread that listens for RPCs from worker.go
//
func (c *Coordinator) server() {
	rpc.Register(c)
	rpc.HandleHTTP()
	//l, e := net.Listen("tcp", ":1234")
	sockname := coordinatorSock()
	os.Remove(sockname)
	l, e := net.Listen("unix", sockname)
	if e != nil {
		log.Fatal("listen error:", e)
	}
	go http.Serve(l, nil)
}

//
// main/mrcoordinator.go calls Done() periodically to find out
// if the entire job has finished.
//
func (c *Coordinator) Done() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.allDone
}

//
// create a Coordinator.
// main/mrcoordinator.go calls this function.
// nReduce is the number of reduce tasks to use.
//
func MakeCoordinator(files []string, nReduce int) *Coordinator {
	c := Coordinator{}

	// 初始化基本配置
	c.files = files
	c.nReduce = nReduce
	c.nMap = len(files)
	c.phase = MapTask
	c.allDone = false
	
	// 初始化Map任务
	c.mapTasks = make([]TaskInfo, c.nMap)
	for i := 0; i < c.nMap; i++ {
		c.mapTasks[i] = TaskInfo{
			TaskId:    i,
			State:     TaskIdle,
			InputFile: files[i],
		}
	}
	c.mapDone = 0
	
	// 初始化Reduce任务
	c.reduceTasks = make([]TaskInfo, c.nReduce)
	for i := 0; i < c.nReduce; i++ {
		c.reduceTasks[i] = TaskInfo{
			TaskId: i,
			State:  TaskIdle,
		}
	}
	c.reduceDone = 0

	// 启动一个goroutine定期检查超时的任务
	go c.checkTimeoutTasks()
	
	c.server()
	return &c
}

// 检查超时的任务并重置它们的状态
func (c *Coordinator) checkTimeoutTasks() {
	for {
		time.Sleep(time.Second) // 每秒检查一次
		
		c.mu.Lock()
		
		// 检查Map任务超时
		for i := 0; i < c.nMap; i++ {
			if c.mapTasks[i].State == TaskInProgress {
				// 如果任务已经执行超过10秒，认为它超时了
				if time.Since(c.mapTasks[i].StartTime) > 10*time.Second {
					log.Printf("Map task %d timeout, reset to idle", i)
					c.mapTasks[i].State = TaskIdle
				}
			}
		}
		
		// 检查Reduce任务超时
		for i := 0; i < c.nReduce; i++ {
			if c.reduceTasks[i].State == TaskInProgress {
				// 如果任务已经执行超过10秒，认为它超时了
				if time.Since(c.reduceTasks[i].StartTime) > 10*time.Second {
					log.Printf("Reduce task %d timeout, reset to idle", i)
					c.reduceTasks[i].State = TaskIdle
				}
			}
		}
		
		c.mu.Unlock()
		
		// 如果所有任务都完成了，退出检查
		if c.Done() {
			return
		}
	}
}
