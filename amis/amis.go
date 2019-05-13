package amis

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/ssm"
	"regexp"
	"sort"
	"strings"
	"time"
)


type com_local ssm.Command

type image_local struct {
	image *ec2.Image
	timestamp time.Time
}

type EC2Connector struct {
	ec2connect *ec2.EC2
}

func (d deleteAMIs) execute(filterstring string) {

	d.creds.getSess()

	d.creds.getCreds()

	d.ec2connect = ec2.New(d.creds.sess, &aws.Config{Credentials: d.creds.creds})

	filter, filterError := makeFilter("")

	if filterError != nil {
		fmt.Println(filterError)
		return
	}

	customAMIs, _ := d.getImg(filter)

	usedAMIs, _ := d.getUsedImg(filter)

	d.deleteImg(d.selectImg(customAMIs, usedAMIs))


}

func (c com_local) get_status(sess *session.Session) string {

	ssmSvc := ssm.New(sess)

	out, _ := ssmSvc.ListCommands(&ssm.ListCommandsInput{CommandId: c.CommandId})

	return *out.Commands[0].Status
}

type status_getter interface {
	get_status(sess *session.Session) string
}

func restart_cwa(role string, configLocation string, sess *session.Session) (*ssm.Command, error) {

	ssmSvc := ssm.New(sess)

	docName := "AmazonCloudWatch-ManageAgent"

	key := "tag:Role"

	nexusTarget := ssm.Target{Key: &key, Values: []*string{&role}}

	targets := []*ssm.Target{&nexusTarget}

	action := "configure"

	mode := "ec2"

	optionalConfigurationSource := "ssm"

	optionalRestart := "yes"

	optionalConfigurationLocation := configLocation

	parameters := make(map[string][]*string)

	parameters["mode"] = []*string{&mode}

	parameters["action"] = []*string{&action}

	parameters["optionalConfigurationSource"] = []*string{&optionalConfigurationSource}

	parameters["optionalRestart"] = []*string{&optionalRestart}

	parameters["optionalConfigurationLocation"] = []*string{&optionalConfigurationLocation}

	ssmresult, ssmerror := ssmSvc.SendCommand(&ssm.SendCommandInput{
		DocumentName: &docName,
		Targets:      targets,
		Parameters:   parameters})

	return ssmresult.Command, ssmerror
}

func status_wait(s status_getter, sess *session.Session) {

	ticker := time.NewTicker(200 * time.Millisecond)

	go func () {
		for range ticker.C {
			stat := s.get_status(sess)
			if stat != "InProgress" {
				ticker.Stop()
				fmt.Println(stat)
				return
			}
		}
	} ()

	time.Sleep(30 * time.Second)
	ticker.Stop()
	return
}

func makeFilter(argfilter string) ([]*ec2.Filter, error) {

	filter := strings.TrimSpace(argfilter)

	filter = strings.Trim(filter, ";")

	filterSlice := make([]*ec2.Filter, 0)

	if filter == "" {
		return filterSlice, nil
	}

	re := regexp.MustCompile("(\\w+)=\\s*([^;]*)")
	//check that the filter string adheres to the format: k=v1,v2;k2=v1

	if re.MatchString(filter) {

		pairs := strings.Split(filter, ";")

		for _, v := range pairs {
			keyValue := strings.Split(v, "=")
			key := keyValue[0]
			values := strings.Split(keyValue[1], ",")

			ptrValues := []*string{}

			for _, v := range values {

				newV := v

				ptrValues = append(ptrValues, &newV)

			}

			filterSlice = append(filterSlice, &ec2.Filter{Name: &key, Values: ptrValues})
		}

		return filterSlice, nil

	}

	return nil, errors.New("filter string invalid.")

}

//Retrieves the details of all AMIs that match filter
func (e EC2Connector) getImg(filter []*ec2.Filter) (map[string][]image_local, error) {

	self := "self"

	input := ec2.DescribeImagesInput{Owners: []*string{&self}, Filters: filter}

	image_map := make(map[string][]image_local)

	if iresult, ierror := e.ec2connect.DescribeImages(&input); ierror != nil {
		return image_map, ierror
	} else {

		for _, image_entry := range iresult.Images {

			img_type := strings.Split(*image_entry.Name, "_")[0]

			tstamp, terror := time.Parse(time.RFC3339, *image_entry.CreationDate)

			if terror != nil {
				return image_map, terror
			} else {
				image_map[img_type] = append(image_map[img_type], image_local{image: image_entry, timestamp: tstamp })
			}

		}

	}

	return image_map, nil
}

//Returns the most recently created image from a list
func (e EC2Connector) getLatest(image_map map[string][]image_local) (image_local) {

	var latest_image image_local

	for _,v := range image_map {
		for _,image := range v {
			if image.timestamp.After(latest_image.timestamp) {
				latest_image = image
			}
		}
	}

	return latest_image
}

//Returns images that are in use.
func (e EC2Connector) getUsedImgWithoutRequest(image_map map[string][]image_local) ([]string, error) {

	instances_in_use := []string{}

	for _,v := range image_map {
		for _,image := range v {
			if image.timestamp.After(latest_image.timestamp) {
				latest_image = image
			}
		}
	}

	if result, error := d.ec2connect.DescribeInstances(&input); error != nil {
		return instances_in_use, error
	} else {
		for _,reservation := range result.Reservations {
			instances_in_use = append(instances_in_use, *reservation.Instances[0].ImageId)
		}
	}
	return instances_in_use, nil
}

//Returns images that are in use.
func (e EC2Connector) getUsedImg(filter []*ec2.Filter) ([]string, error) {

	input := ec2.DescribeInstancesInput{Filters: filter}

	instances_in_use := []string{}

	if result, error := d.ec2connect.DescribeInstances(&input); error != nil {
		return instances_in_use, error
	} else {
		for _,reservation := range result.Reservations {
			instances_in_use = append(instances_in_use, *reservation.Instances[0].ImageId)
		}
	}
	return instances_in_use, nil
}

//Returns images that qualify for deletion: obsolete and not in use
func (d deleteAMIs) selectImg(image_map map[string][]image_local, in_use []string) ([]image_local) {

	qualifiers := make([]image_local, 0)

	for _,v := range image_map {
		sort.Slice(v, func(i,j int) bool {
			return v[i].timestamp.After(v[j].timestamp)
		})

		if len(v) > 3 {
			for _,image := range v[2:] {
				for _, image_in_use := range in_use {
					if *image.image.ImageId != image_in_use {
						qualifiers = append(qualifiers, image)
					}
				}

			}
		}

	}

	return qualifiers
}

//Deletes AMIs
func (d deleteAMIs) deleteImg(images []image_local) error {

	for _, img := range images {
		for _, volume := range img.image.BlockDeviceMappings {
			fmt.Println("DELETING: ", volume.Ebs.SnapshotId)
			_, snaperror := d.ec2connect.DeleteSnapshot(&ec2.DeleteSnapshotInput{SnapshotId: volume.Ebs.SnapshotId})

			if snaperror != nil {
				return snaperror
			}
		}
	}

	for _, img := range images {
		fmt.Println("DELETING: ", *img.image.ImageId)
		_, imgerror := d.ec2connect.DeleteSnapshot(&ec2.DeleteSnapshotInput{SnapshotId: img.image.ImageId})

		if imgerror != nil {
			return imgerror
		}
	}

	return nil
}


//func main() {
//
//	d := deleteAMIs{}
//
//	d.creds.region = "eu-west-2"
//	d.creds.profile = "willem-root"
//	//c.role = "arn:aws:iam::529433045689:role/TestAccountOperatorsRole"
//	//c.serial = "arn:aws:iam::534730700798:mfa/willem.veerman"
//	//c.token = "769725"
//
//	d.creds.getSess()
//
//	d.creds.getCreds()
//
//	fmt.Println(d.creds.token)
//
//
//	fmt.Println("Starting Server")
//
//	router := httprouter.New()
//
//	router.GET("/", index)
//	router.GET("/camis", camis)
//	log.Fatal(http.ListenAndServe(":8081", router))
//
//	//f,_ := makeFilter("")
//	//
//	//i,_ := getImg(c, f)
//	//
//	//fmt.Println("i printed = ", *i["Willemtest1"][0].image)
//	//
//	//a := i["Willemtest1"][0].timestamp
//	//
//	//fmt.Println(a)
//	//
//	//used, _ := getUsedImg(c , f)
//	//
//	//fmt.Println("sel?Img = ", selImg(i, used, c))
//	//
//	//fmt.Println(getUsedImg(c, f))
//
//}
