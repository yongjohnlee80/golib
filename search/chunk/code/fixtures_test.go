package code

const tsSample = `import { a } from "./a";
import b from 'b';

/** Adds two numbers. */
export function add(x: number, y: number): number {
  return x + y;
}

/**
 * A shape.
 */
@Component({ selector: "x" })
export class Shape {
  private name = "s";
  /** The area. */
  area(): number {
    if (this.name === "}") { return 0; }
    return 1;
  }
  static make(): Shape { return new Shape(); }
}

export interface Point { x: number; y: number }

export type Pair<T> = [T, T];

/** Doubles. */
export const double = (n: number): number => {
  return n * 2;
};

console.log("top-level");
`

const jsStrings = "const t = `a ${ x ? '{' : \"}\" } b ${`nested ${y}`} }`;\n" +
	"const re = /function\\s+\\w+{/g;\n" +
	"// function fake() {\n" +
	"/* class Fake { */\n" +
	"function real(a) {\n  const s = \"} function fake() {\";\n  return s + `}`;\n}\n"

const pySample = `"""Module doc."""
import os

# a comment

@decorator
@other(
    arg=1,
)
def top(a, b: int) -> int:
    """Top adds."""
    if a:
        return a
    return b


class Box(Base):
    """A box."""

    size = 3

    def open(self):
        """Opens it."""
        return self.size

    async def close(self):
        s = """
def not_a_def():
class NotAClass:
"""
        return s

print("done")
`

const pyStrings = "x = '''\ndef inside():\n'''\n" +
	"def real():\n    # def fake():\n    s = \"def fake(): # not a comment\"\n    return s\n" +
	"y = f\"{1}\" # tail\n"

const rsSample = `//! Crate doc.
use std::fmt;

/// A point.
#[derive(Debug, Clone)]
pub struct Point {
    x: i32,
    y: i32,
}

pub struct Unit;

/// Kinds.
enum Kind { A, B }

/// Shapes have areas.
pub trait Shape {
    fn area(&self) -> f64;
}

impl fmt::Display for Point {
    /// Formats.
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        write!(f, "({}, {})", self.x, self.y)
    }
}

impl Point {
    const ORIGIN: i32 = 0;

    pub fn new(x: i32, y: i32) -> Self {
        Point { x, y }
    }

    pub(crate) async fn later<'a>(&'a self) -> &'a i32 {
        &self.x
    }
}

/// Main.
pub const fn answer() -> i32 { 42 }

extern "C" fn callback() {}

macro_rules! m { () => {} }
`

const rsStrings = "fn real() {\n    let s = \"} fn fake() {\";\n    let r = r#\"} fn fake2() \"{\"#;\n" +
	"    let c = '}';\n    let l: &'static str = \"x\";\n    /* fn fake3() { /* nested */ } */\n}\n" +
	"fn second() {}\n"
