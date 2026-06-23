package main

import "core:log"

main :: proc() {
    logger := log.create_console_logger()
	context.logger = logger
	log.infof("hellope")
}
