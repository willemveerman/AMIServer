package credentials

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
)

type CredsObject struct {
	region string
	profile string
	role string
	serial string
	token string
	sess *session.Session
	creds *credentials.Credentials
	error error
}

func (c *CredsObject) getSess() {
	c.sess, c.error = session.NewSession(&aws.Config{
		Region:      aws.String(c.region),
		Credentials: credentials.NewSharedCredentials("", c.profile),
	})
}

func (c *CredsObject) getCreds() {
	c.creds = stscreds.NewCredentials(c.sess, c.role, func(p *stscreds.AssumeRoleProvider) {
		p.SerialNumber = aws.String(c.serial)
		p.TokenCode = aws.String(c.token)
	})
}
