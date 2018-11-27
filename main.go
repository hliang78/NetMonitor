package NetMonitor

import (
	"fmt"
	"reflect"
)

//主程序入口
func main() {
	fmt.Println("hello world")
	a := make([]byte, 10)
	fmt.Println(reflect.TypeOf(a))
}
