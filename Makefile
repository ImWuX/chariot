.PHONY: all clean

all: clean chariot

chariot:
	gcc -std=gnu2x $(shell find ./src -type f -name "*c") -o $@

clean:
	rm -f chariot