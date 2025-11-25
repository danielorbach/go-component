# Golang System Components

[![CI](https://github.com/danielorbach/go-component/actions/workflows/ci.yml/badge.svg)](https://github.com/danielorbach/go-component/actions/workflows/ci.yml)

This library provides a set of types and functions to help enterprise developers describe, deploy, and test the Golang components that build up their software system.

## Describing components

The basic building block of a backend system is a component. A component is a deployable piece of message-driven code with the capabilities to **observe and react** to events in an enterprise's ecosystem.

Each component has the potential to **notify** other components about changes to its exposed _Aspect_. We say that those components have an _Interest_ to **track** the component's _Aspect_. As such, a component may have none, one or many _Aspects_ and _Interests_.

## Acknowledgments

Special thanks to @ofektavor, @yuvalmendelovich, @marombracha, and @arieltod for your contributions and collaboration on this project.

A very special thank you to @sgebbie; many of the concepts in this project came from working together and drawing from your deep experience in systems engineering.

A very special thank you to @tal-shani for being there from the beginning. Your unwavering support, thoughtful guidance, and genuine partnership have been invaluable throughout this journey.

Working with you all was a pleasure, and your fingerprints are all over the good parts of this codebase. Cheers!
