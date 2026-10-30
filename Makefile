ifeq ($(OS),Windows_NT)
EXE := .exe
GUIH := -ldflags=-H=windowsgui
else ifneq (,$(findstring _NT,$(shell uname -s)))
EXE := .exe
GUIH := -ldflags=-H=windowsgui
endif

CXX      ?= g++
CXXFLAGS ?= -O2 -fPIC -fno-exceptions -fno-rtti
CPPFLAGS += -Iclib

SRCS := $(wildcard clib/*.cpp clib/Chris/*.cpp clib/Chris/DNS/*.cpp clib/Jochen/*.cpp)
OBJS := $(SRCS:.cpp=.o)

AA_SRCS := $(wildcard third_party/atariaudio/src/*.cpp third_party/atariaudio/src/external/*.cpp third_party/atariaudio/src/external/Musashi/*.cpp)
AA_OBJS := $(AA_SRCS:.cpp=.aa.o)

LIB := lib/libtfmxdec.a
DELILIB := lib/libdeli.a
AALIB := lib/libatariaudio.a

CC       ?= gcc
CFLAGS   ?= -O2 -fPIC -w
DELI_SRCS := $(wildcard clib/deli/*.c) \
	clib/musashi/m68kcpu.c clib/musashi/m68kops.c clib/musashi/softfloat/softfloat.c
DELI_SRCS := $(filter-out clib/deli/test_deli.c,$(DELI_SRCS))
DELI_OBJS := $(DELI_SRCS:.c=.deli.o)

.PHONY: all clean

all: $(LIB) $(DELILIB) $(AALIB) player gui

$(LIB): $(OBJS)
	@mkdir -p lib
	ar rcs $@ $(OBJS)

$(DELILIB): $(DELI_OBJS)
	@mkdir -p lib
	ar rcs $@ $(DELI_OBJS)

$(AALIB): $(AA_OBJS)
	@mkdir -p lib
	ar rcs $@ $(AA_OBJS)
	objcopy --redefine-syms third_party/atariaudio/rename.map $@

%.aa.o: %.cpp
	$(CXX) $(CXXFLAGS) -Ithird_party/atariaudio/src -c $< -o $@

%.deli.o: %.c
	$(CC) $(CFLAGS) -Iclib -Iclib/musashi -Iclib/deli -c $< -o $@

%.o: %.cpp
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c $< -o $@

player: $(LIB) $(DELILIB) $(AALIB)
	go build -o bin/amiga-player$(EXE) ./cmd/amiga-player

gui: $(LIB) $(DELILIB) $(AALIB)
	go build -o bin/chiptune-gui$(EXE) $(GUIH) ./cmd/chiptune-gui

clean:
	rm -f $(OBJS) $(DELI_OBJS) $(AA_OBJS) $(LIB) $(DELILIB) $(AALIB) bin/amiga-player$(EXE) bin/chiptune-gui$(EXE)
