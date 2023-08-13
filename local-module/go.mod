module local-module

go 1.18

require (
  local-module.com/src v0.0.0
  local-module.com/tests v0.0.0
)

replace (
  local-module.com/src v0.0.0 => ./src
  local-module.com/tests v0.0.0 => ./tests
)