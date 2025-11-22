# Golang System Components

[![CI](https://github.com/danielorbach/go-component/actions/workflows/ci.yml/badge.svg)](https://github.com/danielorbach/go-component/actions/workflows/ci.yml)

This library provides a set of types and functions to help enterprise developers describe, deploy, and test the Golang components that build up their software system.

## Describing components

The basic building block of a backend system is a component. A component is a deployable piece of message-driven code with the capabilities to **observe and react** to events in an enterprise's ecosystem.

Each component has the potential to **notify** other components about changes to its exposed _Aspect_. We say that those components have an _Interest_ to **track** the component's _Aspect_. As such, a component may have none, one or many _Aspects_ and _Interests_.

## Acknowledgments

Special thanks to [@ofektavor](https://github.com/ofektavor), [@yuvalmendelovich](https://github.com/yuvalmendelovich), [@marombracha](https://github.com/marombracha), and [@arieltod](https://github.com/arieltod).

A very special thank you to [@sgebbie](https://github.com/sgebbie), whose deep systems engineering experience shaped many of the core concepts here, and to [@tal-shani](https://github.com/tal-shani), whose insight, persistence, and architectural clarity materially improved the robustness and direction of this codebase.

Working with you all was a pleasure, and your fingerprints are all over the good parts of this codebase. Cheers!
