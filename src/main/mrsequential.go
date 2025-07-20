package main

//
// 简单的顺序化 MapReduce 实现。
// 这是一个单机版本的 MapReduce，用于演示基本的 MapReduce 工作流程。
//
// 使用方法: go run mrsequential.go wc.so pg*.txt
// 其中 wc.so 是包含 Map 和 Reduce 函数的插件文件
// pg*.txt 是输入文件
//

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"plugin"
	"sort"

	"6.5840/mr"
)

// ByKey 类型用于按键排序的 KeyValue 切片
// 实现了 sort.Interface 接口，使得可以使用 sort.Sort() 函数
type ByKey []mr.KeyValue

// 以下三个方法实现了 sort.Interface 接口，用于按键排序
func (a ByKey) Len() int           { return len(a) }
func (a ByKey) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByKey) Less(i, j int) bool { return a[i].Key < a[j].Key }

func main() {
	// 检查命令行参数是否足够（至少需要程序名、插件文件和一个输入文件）
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: mrsequential xxx.so inputfiles...\n")
		os.Exit(1)
	}

	// 从插件文件中加载 Map 和 Reduce 函数
	mapf, reducef := loadPlugin(os.Args[1])

	//
	// Map 阶段：
	// 1. 读取每个输入文件
	// 2. 将文件内容传递给 Map 函数
	// 3. 收集所有 Map 函数产生的中间键值对
	//
	// intermediate 用于存储所有 Map 输出的键值对
	intermediate := []mr.KeyValue{}
	// 遍历所有输入文件（从命令行参数的第3个开始）
	for _, filename := range os.Args[2:] {
		// 打开文件
		file, err := os.Open(filename)
		if err != nil {
			log.Fatalf("cannot open %v", filename)
		}
		// 读取文件的全部内容
		content, err := ioutil.ReadAll(file)
		if err != nil {
			log.Fatalf("cannot read %v", filename)
		}
		file.Close()
		// 调用 Map 函数处理文件内容，得到键值对数组
		kva := mapf(filename, string(content))
		// 将这些键值对添加到中间结果中
		intermediate = append(intermediate, kva...)
	}

	//
	// 注意：这与真正的分布式 MapReduce 有很大区别
	// 在真正的 MapReduce 中，中间数据会被分区到 N×M 个桶中
	// 而这里所有的中间数据都存储在一个数组 intermediate[] 中
	//

	// 按键对所有中间结果进行排序
	// 这样相同的键会聚集在一起，便于后续的 Reduce 操作
	sort.Sort(ByKey(intermediate))

	// 创建输出文件
	// 在真正的 MapReduce 中会有多个输出文件，这里简化为一个
	oname := "mr-out-0"
	ofile, _ := os.Create(oname)

	//
	// Reduce 阶段：
	// 对 intermediate[] 中的每个不同的键调用 Reduce 函数
	// 并将结果输出到 mr-out-0 文件
	//
	i := 0
	for i < len(intermediate) {
		// 找到所有具有相同键的键值对的范围 [i, j)
		j := i + 1
		for j < len(intermediate) && intermediate[j].Key == intermediate[i].Key {
			j++
		}
		// 收集相同键的所有值
		values := []string{}
		for k := i; k < j; k++ {
			values = append(values, intermediate[k].Value)
		}
		// 调用 Reduce 函数处理这个键及其所有值
		output := reducef(intermediate[i].Key, values)

		// 按照正确的格式输出每行 Reduce 结果：键 值
		fmt.Fprintf(ofile, "%v %v\n", intermediate[i].Key, output)

		// 移动到下一个不同的键
		i = j
	}

	ofile.Close()
}

// loadPlugin 从插件文件中加载应用程序的 Map 和 Reduce 函数
// 插件文件通常是一个编译好的 .so 文件，例如 ../mrapps/wc.so
// 返回值是 Map 函数和 Reduce 函数
func loadPlugin(filename string) (func(string, string) []mr.KeyValue, func(string, []string) string) {
	// 打开插件文件
	p, err := plugin.Open(filename)
	if err != nil {
		log.Fatalf("cannot load plugin %v", filename)
	}
	// 查找插件中的 Map 函数
	xmapf, err := p.Lookup("Map")
	if err != nil {
		log.Fatalf("cannot find Map in %v", filename)
	}
	// 将 Map 函数转换为正确的函数类型
	// Map 函数接收文件名和内容，返回键值对数组
	mapf := xmapf.(func(string, string) []mr.KeyValue)
	// 查找插件中的 Reduce 函数
	xreducef, err := p.Lookup("Reduce")
	if err != nil {
		log.Fatalf("cannot find Reduce in %v", filename)
	}
	// 将 Reduce 函数转换为正确的函数类型
	// Reduce 函数接收键和该键对应的所有值，返回聚合后的结果
	reducef := xreducef.(func(string, []string) string)

	return mapf, reducef
}
