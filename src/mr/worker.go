package mr

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"net/rpc"
	"os"
	"sort"
	"time"
)

// Map functions return a slice of KeyValue.
type KeyValue struct {
	Key   string
	Value string
}

// use ihash(key) % NReduce to choose the reduce
// task number for each KeyValue emitted by Map.
func ihash(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() & 0x7fffffff)
}

// main/mrworker.go calls this function.
func Worker(mapf func(string, string) []KeyValue,
	reducef func(string, []string) string) {

	// Worker主循环：请求任务并执行
	for {
		// 向Coordinator请求任务
		task := getTask()

		// 根据任务类型执行相应操作
		switch task.TaskType {
		case MapTask:
			performMapTask(mapf, &task)
		case ReduceTask:
			performReduceTask(reducef, &task)
		case WaitTask:
			// 等待其他任务完成
			time.Sleep(time.Second)
		case ExitTask:
			// 所有任务完成，退出
			return
		default:
			log.Fatalf("Unknown task type: %v", task.TaskType)
		}
	}
}

// 向Coordinator请求任务
func getTask() Task {
	args := GetTaskArgs{}
	reply := GetTaskReply{}

	ok := call("Coordinator.GetTask", &args, &reply)
	if !ok {
		log.Fatalf("Failed to get task from coordinator")
	}

	return reply.Task
}

// 执行Map任务
func performMapTask(mapf func(string, string) []KeyValue, task *Task) {
	// log.Printf("Performing Map task %d on file %s", task.TaskId, task.InputFile)

	// 步骤1: 读取输入文件
	file, err := os.Open(task.InputFile)
	if err != nil {
		log.Printf("Failed to open file %v: %v", task.InputFile, err)
		reportTask(task.TaskType, task.TaskId, false)
		return
	}
	content, err := ioutil.ReadAll(file)
	file.Close()
	if err != nil {
		log.Printf("Failed to read file %v: %v", task.InputFile, err)
		reportTask(task.TaskType, task.TaskId, false)
		return
	}

	// 步骤2: 调用Map函数处理文件内容
	kva := mapf(task.InputFile, string(content))
	// log.Printf("Map task %d produced %d key-value pairs", task.TaskId, len(kva))

	// 步骤3: 根据key的hash值将结果分配到不同的桶（bucket）
	// 创建nReduce个桶，每个桶对应一个reduce任务
	buckets := make([][]KeyValue, task.NReduce)
	for i := 0; i < task.NReduce; i++ {
		buckets[i] = []KeyValue{}
	}

	// 将每个键值对分配到对应的桶
	for _, kv := range kva {
		// 使用ihash函数计算这个key应该分配给哪个reduce任务
		bucketIndex := ihash(kv.Key) % task.NReduce
		buckets[bucketIndex] = append(buckets[bucketIndex], kv)
	}

	// 步骤4: 将每个桶的内容写入对应的中间文件
	for i := 0; i < task.NReduce; i++ {
		// 中间文件名格式: mr-X-Y，X是map任务ID，Y是reduce任务ID
		filename := fmt.Sprintf("mr-%d-%d", task.TaskId, i)

		file, err := os.Create(filename)
		if err != nil {
			log.Printf("Failed to create intermediate file %v: %v", filename, err)
			reportTask(task.TaskType, task.TaskId, false)
			return
		}

		// 使用JSON编码器写入键值对
		encoder := json.NewEncoder(file)
		for _, kv := range buckets[i] {
			err := encoder.Encode(&kv)
			if err != nil {
				log.Printf("Failed to encode kv pair: %v", err)
				file.Close()
				reportTask(task.TaskType, task.TaskId, false)
				return
			}
		}

		file.Close()
		// log.Printf("Map task %d: wrote %d pairs to %s", task.TaskId, len(buckets[i]), filename)
	}

	// 所有中间文件都成功写入，报告任务完成
	reportTask(task.TaskType, task.TaskId, true)
}

// 执行Reduce任务
func performReduceTask(reducef func(string, []string) string, task *Task) {
	// log.Printf("Performing Reduce task %d", task.TaskId)

	// 步骤1: 读取所有Map任务为这个Reduce任务生成的中间文件
	kva := []KeyValue{}
	for mapTaskId := 0; mapTaskId < task.NMap; mapTaskId++ {
		// 中间文件名格式: mr-X-Y，X是map任务ID，Y是reduce任务ID
		filename := fmt.Sprintf("mr-%d-%d", mapTaskId, task.TaskId)

		file, err := os.Open(filename)
		if err != nil {
			log.Printf("Failed to open intermediate file %v: %v", filename, err)
			// 如果文件不存在，可能是Map任务还没完成，继续尝试其他文件
			continue
		}

		// 使用JSON解码器读取键值对
		decoder := json.NewDecoder(file)
		for {
			var kv KeyValue
			if err := decoder.Decode(&kv); err != nil {
				// 读到文件末尾或出错，退出循环
				break
			}
			kva = append(kva, kv)
		}
		file.Close()
		// log.Printf("Reduce task %d: read %s", task.TaskId, filename)
	}

	// log.Printf("Reduce task %d: total %d key-value pairs", task.TaskId, len(kva))

	// 步骤2: 按键排序，让相同的键聚集在一起
	sort.Slice(kva, func(i, j int) bool {
		return kva[i].Key < kva[j].Key
	})

	// 步骤3: 创建输出文件
	outputFileName := fmt.Sprintf("mr-out-%d", task.TaskId)
	outputFile, err := os.Create(outputFileName)
	if err != nil {
		log.Printf("Failed to create output file %v: %v", outputFileName, err)
		reportTask(task.TaskType, task.TaskId, false)
		return
	}

	// 步骤4: 对每个不同的键调用reduce函数
	i := 0
	for i < len(kva) {
		// 找出所有具有相同键的值
		j := i + 1
		for j < len(kva) && kva[j].Key == kva[i].Key {
			j++
		}

		// 收集这个键的所有值
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, kva[k].Value)
		}

		// 调用reduce函数
		output := reducef(kva[i].Key, values)

		// 写入输出文件
		fmt.Fprintf(outputFile, "%v %v\n", kva[i].Key, output)

		// 移动到下一个不同的键
		i = j
	}

	outputFile.Close()
	// log.Printf("Reduce task %d: wrote output to %s", task.TaskId, outputFileName)

	// 报告任务完成
	reportTask(task.TaskType, task.TaskId, true)
}

// 向Coordinator报告任务完成
func reportTask(taskType TaskType, taskId int, success bool) {
	args := ReportTaskArgs{
		TaskType: taskType,
		TaskId:   taskId,
		Success:  success,
	}
	reply := ReportTaskReply{}

	ok := call("Coordinator.ReportTask", &args, &reply)
	if !ok {
		log.Printf("Failed to report task completion")
	}
}

// example function to show how to make an RPC call to the coordinator.
//
// the RPC argument and reply types are defined in rpc.go.
func CallExample() {

	// declare an argument structure.
	args := ExampleArgs{}

	// fill in the argument(s).
	args.X = 99

	// declare a reply structure.
	reply := ExampleReply{}

	// send the RPC request, wait for the reply.
	// the "Coordinator.Example" tells the
	// receiving server that we'd like to call
	// the Example() method of struct Coordinator.
	ok := call("Coordinator.Example", &args, &reply)
	if ok {
		// reply.Y should be 100.
		fmt.Printf("reply.Y %v\n", reply.Y)
	} else {
		fmt.Printf("call failed!\n")
	}
}

// send an RPC request to the coordinator, wait for the response.
// usually returns true.
// returns false if something goes wrong.
func call(rpcname string, args interface{}, reply interface{}) bool {
	// c, err := rpc.DialHTTP("tcp", "127.0.0.1"+":1234")
	sockname := coordinatorSock()
	c, err := rpc.DialHTTP("unix", sockname)
	if err != nil {
		log.Fatal("dialing:", err)
	}
	defer c.Close()

	err = c.Call(rpcname, args, reply)
	if err == nil {
		return true
	}

	fmt.Println(err)
	return false
}
