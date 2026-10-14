# Policies in `terraform show -json` output (plan or state), one line each,
# fields separated by U+001F (empty fields survive `read`):
# address, Access Analyzer policy type, validate-policy resource type, document.
# planned_values (plan) or values (state) hold rendered attributes; a plan omits
# values that are unknown until apply, which after_unknown marks ("UNKNOWN").
def kind:
  { "aws_iam_policy":                   ["policy", "IDENTITY_POLICY", ""],
    "aws_iam_role_policy":              ["policy", "IDENTITY_POLICY", ""],
    "aws_iam_role":                     ["assume_role_policy", "RESOURCE_POLICY", "AWS::IAM::AssumeRolePolicyDocument"],
    "aws_s3_bucket_policy":             ["policy", "RESOURCE_POLICY", "AWS::S3::Bucket"],
    "aws_kms_key":                      ["policy", "RESOURCE_POLICY", ""],
    "aws_sqs_queue_policy":             ["policy", "RESOURCE_POLICY", ""],
    "aws_sns_topic_policy":             ["policy", "RESOURCE_POLICY", ""],
    "aws_secretsmanager_secret_policy": ["policy", "RESOURCE_POLICY", ""],
    "aws_vpc_endpoint":                 ["policy", "RESOURCE_POLICY", ""] }[.type];
((.resource_changes // []) | map({(.address): (.change.after_unknown // {})}) | add // {}) as $unknown |
def modules: ., (.child_modules[]? | modules);
[ (.planned_values // .values).root_module | modules | .resources[]? ]
| map(select(.mode == "managed" and kind != null)) | .[]
| kind as [$attr, $ptype, $rtype]
| .address as $addr
| if (.values[$attr] // "") != "" then [$addr, $ptype, $rtype, .values[$attr]]
  elif ($unknown[$addr][$attr] // false) == true then [$addr, $ptype, $rtype, "UNKNOWN"]
  else empty end
| map(gsub("\n"; " ")) | join("\u001f")
