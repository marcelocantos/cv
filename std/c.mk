cc ?= cc
cflags ?= -Wall
ldflags ?=
ar ?= ar
ccache ?= $[shell command -v ccache 2>/dev/null]

{name}.o [deps: gcc]: {name}.c
    $ccache $cc $cflags -MMD -MF $depfile -c $input -o $target
