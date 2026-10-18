CXX      ?= g++
CXXFLAGS ?= -O2 -fPIC -fno-exceptions -fno-rtti
CPPFLAGS += -Iclib

SRCS := $(wildcard clib/*.cpp clib/Chris/*.cpp clib/Chris/DNS/*.cpp clib/Jochen/*.cpp)
OBJS := $(SRCS:.cpp=.o)

LIB := lib/libtfmxdec.a

.PHONY: all clean

all: $(LIB) player gui

$(LIB): $(OBJS)
	@mkdir -p lib
	ar rcs $@ $(OBJS)

%.o: %.cpp
	$(CXX) $(CPPFLAGS) $(CXXFLAGS) -c $< -o $@

player: $(LIB)
	go build -o bin/amiga-player ./cmd/amiga-player

gui: $(LIB)
	go build -o bin/chiptune-gui ./cmd/chiptune-gui

clean:
	rm -f $(OBJS) $(LIB) bin/amiga-player bin/chiptune-gui
