package mr

//
// RPC definitions.
//
// remember to capitalize all names.
//

import "os"
import "strconv"

//
// example to show how to declare the arguments
// and reply for an RPC.
//

type ExampleArgs struct {
	X int
}

type ExampleReply struct {
	Y int
}

// Task types
type TaskType int

const (
	MapTask TaskType = iota
	ReduceTask
	WaitTask  // 等待其他任务完成
	ExitTask  // 所有任务完成，worker可以退出
)

// Task information structure
type Task struct {
	TaskType   TaskType
	TaskId     int      // 任务ID
	InputFile  string   // Map任务的输入文件
	OutputFile string   // Reduce任务的输出文件
	NReduce    int      // Reduce任务数量
	NMap       int      // Map任务数量
}

// Worker请求任务的RPC结构
type GetTaskArgs struct {
	// Worker可以发送自己的标识符，这里暂时为空
}

type GetTaskReply struct {
	Task Task
}

// Worker报告任务完成的RPC结构
type ReportTaskArgs struct {
	TaskType TaskType
	TaskId   int
	Success  bool // 任务是否成功完成
}

type ReportTaskReply struct {
	// 暂时为空，可以用于确认
}


// Cook up a unique-ish UNIX-domain socket name
// in /var/tmp, for the coordinator.
// Can't use the current directory since
// Athena AFS doesn't support UNIX-domain sockets.
func coordinatorSock() string {
	s := "/var/tmp/5840-mr-"
	s += strconv.Itoa(os.Getuid())
	return s
}
