# Module: domain/utils/dtos/project.go

## Purpose

Provides reflection-based DTO projection helpers.

## Responsibilities

- Project one struct or map into a caller-selected DTO struct.
- Project slices or arrays into DTO slices.
- Match fields by Go name, lower-camel name, snake-case name, and `json`, `form`, or `query` tag names.
- Copy assignable or convertible scalar values, including pointer targets where compatible.
- Reject unsupported projection sources and non-struct DTO targets with explicit errors.
