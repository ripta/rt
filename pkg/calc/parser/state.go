package parser

type parsingState func(*P) parsingState
