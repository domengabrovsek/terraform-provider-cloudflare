resource "cloudflare_pages_project" "%[1]s" {
	account_id = "%[2]s"
	name = "%[3]s"
	production_branch = "main"
	deployment_configs = {
		preview = {
			env_vars = {
				FOO = {
					type = "plain_text"
					value = "bar"
				}
			}
		}
		production = {
			env_vars = {
				FOO = {
					type = "plain_text"
					value = "bar"
				}
			}
		}
	}
}
