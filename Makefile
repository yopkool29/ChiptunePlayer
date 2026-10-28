CXX      ?= g++
CXXFLAGS ?= -O2 -fPIC -fno-exceptions -fno-rtti
CPPFLAGS += -Iclib

SRCS := $(wildcard clib/*.cpp clib/Chris/*.cpp clib/Chris/DNS/*.cpp clib/Jochen/*.cpp)
OBJS := $(SRCS:.cpp=.o)

LIB := lib/libtfmxdec.a
DELILIB := lib/libdeli.a

CC       ?= gcc
CFLAGS   ?= -O2 -fPIC -w
DELI_SRCS := $(wildcard clib/deli/*.c) \
	clib/musashi/m68kcpu.c clib/musashi/m68kops.c clib/musashi/softfloat/softfloat.c
DELI_SRCS := $(filter-out clib/deli/test_deli.c,$(DELI_SRCS))
DELI_OBJS := $(DELI_SRCS:.c=.deli.o)

.PHONY: all clean

all: $(LIB) $(DELILIB) player gui

$(LIB): $(OBJS)
	@mkdir -p lib
	ar rcs $@ $(OBJS)

$(DELILIB): $(DELI_OBJS)
	@mkdir -p lib
	ar rcs $@ $(DELI_OBJS)

%.deli.o: %.c
	$(CC) $(CFLAGS) -Iclib -Iclib/musashi -Iclib/deli -c $< -o $@

%.o: %.cpp
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c $< -o $@

player: $(LIB) $(DELILIB)
	go build -o bin/amiga-player ./cmd/amiga-player

gui: $(LIB) $(DELILIB)
	go build -o bin/chiptune-gui ./cmd/chiptune-gui

clean:
	rm -f $(OBJS) $(DELI_OBJS) $(LIB) $(DELILIB) bin/amiga-player bin/chiptune-gui
