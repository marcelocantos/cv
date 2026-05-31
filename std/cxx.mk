cxx ?= c++
cxxflags ?= -Wall
ldflags ?=
ccache ?= $[shell command -v ccache 2>/dev/null]

{name}.o [deps: gcc]: {name}.cc
    $ccache $cxx $cxxflags -MMD -MF $depfile -c $input -o $target
