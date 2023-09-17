# init

go install github.com/swaggo/swag/cmd/swag@latest

# before build / debug run

swag init

# importing docs

```
package main

import (
	// _ "(root module name)/docs"
	_ "test-swagger/docs"
)

func main() {
	...
}
```