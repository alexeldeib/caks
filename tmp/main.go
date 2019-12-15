package main

import (
	"fmt"
	"strings"
)

func main() {
	id := "azure:///subscriptions/9cff731e-4a39-42e0-9835-543ad13ab87b/resourceGroups/draft-prod-eus-v2/providers/Microsoft.Compute/virtualMachineScaleSets/default-ae90a13e-2bd64e0d/virtualMachines/3"
	tokens := strings.Split(id, ":")
	fmt.Printf("%#v\n", strings.Split(tokens[1], "/")[3:])
	fmt.Println(len(strings.Split(tokens[1], "/")[3:]))
}
