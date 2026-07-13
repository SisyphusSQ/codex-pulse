#!/usr/bin/env ruby

require "yaml"

def reject(message)
  warn message
  exit 1
end

def mapping_pairs(node, context)
  reject("#{context} must be a YAML mapping") unless node.is_a?(Psych::Nodes::Mapping)

  node.children.each_slice(2).to_a
end

def key_matches?(node, expected)
  node.is_a?(Psych::Nodes::Scalar) && node.value == expected
end

def collect_strings(value, strings = [])
  case value
  when String
    strings << value
  when Hash
    value.each do |key, child|
      collect_strings(key, strings)
      collect_strings(child, strings)
    end
  when Array
    value.each { |child| collect_strings(child, strings) }
  end

  strings
end

begin
  path = ARGV.fetch(0)
  source = File.read(path)
  document_node = Psych.parse(source, filename: path)
  reject("workflow must contain one YAML document") unless document_node

  root_pairs = mapping_pairs(document_node.root, "workflow root")
  permission_pairs = root_pairs.select { |key, _value| key_matches?(key, "permissions") }
  reject("workflow must define exactly one top-level permissions map") unless permission_pairs.length == 1

  permission_entries = mapping_pairs(permission_pairs.first.last, "top-level permissions")
  unless permission_entries.length == 1 && key_matches?(permission_entries.first.first, "contents")
    reject("top-level permissions must define the contents key exactly once")
  end

  jobs_pairs = root_pairs.select { |key, _value| key_matches?(key, "jobs") }
  reject("workflow must define exactly one jobs map") unless jobs_pairs.length == 1

  mapping_pairs(jobs_pairs.first.last, "jobs").each do |job_key, job_node|
    job_name = job_key.is_a?(Psych::Nodes::Scalar) ? job_key.value : "unknown"
    job_pairs = mapping_pairs(job_node, "job #{job_name}")
    next unless job_pairs.any? { |key, _value| key_matches?(key, "permissions") }

    reject("job #{job_name} must not override permissions")
  end

  workflow = YAML.safe_load(
    source,
    permitted_classes: [],
    permitted_symbols: [],
    aliases: false,
    filename: path
  )
  reject("workflow root must be a YAML mapping") unless workflow.is_a?(Hash)

  expected_permissions = { "contents" => "read" }
  reject("top-level permissions must contain only contents: read") unless workflow["permissions"] == expected_permissions

  jobs = workflow["jobs"]
  reject("jobs must safely load as a YAML mapping") unless jobs.is_a?(Hash)
  jobs.each do |job_name, job|
    reject("job #{job_name} must safely load as a YAML mapping") unless job.is_a?(Hash)
    reject("job #{job_name} must not override permissions") if job.key?("permissions")
  end

  expressions = collect_strings(workflow).flat_map do |value|
    value.scan(/\$\{\{\s*(.*?)\s*\}\}/m).flatten.map(&:strip)
  end
  allowed_expressions = ["github.workflow", "github.ref"]
  unless expressions.sort == allowed_expressions.sort
    reject("workflow expressions must be limited to github.workflow and github.ref, once each")
  end
rescue Errno::ENOENT, KeyError, Psych::Exception, ArgumentError => error
  reject("cannot safely parse workflow YAML: #{error.message}")
end
