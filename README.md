# AWSDocs Archive

This tool allows you to be able to retrieve all documentation for AWS providing you with a local copy you can archive, search, and diff for security research. I used this repository to create a Bedrock Knowledge Base for querying with AI.

- Retrieves all sitemap.xml files
- Recursively retrieves all links within them
- Ignores all URLs included in the sitemaps that do not include `docs.aws.amazon.com`
- Ignores all non https links
- Avoids most AWS SDK documentation
- Supports both outputting as warc or html file formats
- Saves all files by `aws_warcs/` or `aws_html/` and `YYYY/MM/DD/docs.aws.amazon.com/ec2/index.warc`

## Usage

The following command allows you to be able to retrieve all the documentation in `aws_warcs/YYYY/MM/DD`.

```bash
awsdocs --rate-limit --workers 15 -logfile=awsdocs.log
```

## Searching

One thing I discovered as part of this project was [ripgrep](https://github.com/BurntSushi/ripgrep) which helped massively reduce the time to search through all the files recursively as quickly as possible. Grep took `36.78s` and ripgrep spent `0.67s` for the exact same search. So I strongly advise getting familiar with ripgrep to help speed up your search. 

## Retrieve URLs From Query

To search for a specific string and retrieve all AWS Documentation urls containing that string you can use a combination of ripgrep and xargs to do so. 

```bash
$ cd 2024/09/26/docs.aws.amazon.com
$ rg "s3://amzn-s3-demo-bucket-" . -l | xargs -I {} rg "Warc-Target-Uri" {} | awk '{print $2}' | sort | uniq
https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries_WritingCanary_Nodejs.html
https://docs.aws.amazon.com/athena/latest/ug/tables-location-format.html
https://docs.aws.amazon.com/bedrock/latest/userguide/batch-inference-example.html
```

## Simple Search

```bash
$ rg "s3://amzn-s3-demo-bucket-" .
./athena/latest/ug/tables-location-format.warc
101:        <b>Use</b>:</p><pre class="programlisting"><div class="code-btn-container"></div><!--DEBUG: cli ()--><code class="nohighlight">s3://amzn-s3-demo-bucket/<code class="replaceable">folder</code>/</code></pre><pre class="programlisting"><div class="code-btn-container"></div><!--DEBUG: cli ()--><code class="nohighlight">s3://amzn-s3-demo-bucket-<code class="replaceable">metadata</code>-s3alias/<code class="replaceable">folder</code>/</code></pre><p>Do not use any of the following items for specifying the <code class="code">LOCATION</code> for your

./bedrock/latest/userguide/batch-inference-example.warc
95:        "s3Uri": "s3://amzn-s3-demo-bucket-input/abc.jsonl"
101:        "s3Uri": "s3://amzn-s3-demo-bucket-output/"

./AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries_WritingCanary_Nodejs.warc
337:   "ArtifactS3Location":"s3://amzn-s3-demo-bucket-123456789012-us-west-2",
```
