package aws

import (
	"bytes"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/hashicorp/terraform/helper/hashcode"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
)

func resourceAwsDynamoDbTable() *schema.Resource {
	return &schema.Resource{
		Create: resourceAwsDynamoDbTableCreate,
		Read:   resourceAwsDynamoDbTableRead,
		Update: resourceAwsDynamoDbTableUpdate,
		Delete: resourceAwsDynamoDbTableDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		SchemaVersion: 1,
		MigrateState:  resourceAwsDynamoDbTableMigrateState,

		Schema: map[string]*schema.Schema{
			"arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"hash_key": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},
			"range_key": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},
			"write_capacity": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"read_capacity": {
				Type:     schema.TypeInt,
				Required: true,
			},
			"attribute": {
				Type:     schema.TypeSet,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"type": {
							Type:     schema.TypeString,
							Required: true,
						},
					},
				},
				Set: func(v interface{}) int {
					var buf bytes.Buffer
					m := v.(map[string]interface{})
					buf.WriteString(fmt.Sprintf("%s-", m["name"].(string)))
					return hashcode.String(buf.String())
				},
			},
			"ttl": {
				Type:     schema.TypeSet,
				Optional: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"attribute_name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"enabled": {
							Type:     schema.TypeBool,
							Required: true,
						},
					},
				},
			},
			"local_secondary_index": {
				Type:     schema.TypeSet,
				Optional: true,
				ForceNew: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"range_key": {
							Type:     schema.TypeString,
							Required: true,
						},
						"projection_type": {
							Type:     schema.TypeString,
							Required: true,
						},
						"non_key_attributes": {
							Type:     schema.TypeList,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
					},
				},
				Set: func(v interface{}) int {
					var buf bytes.Buffer
					m := v.(map[string]interface{})
					buf.WriteString(fmt.Sprintf("%s-", m["name"].(string)))
					return hashcode.String(buf.String())
				},
			},
			"global_secondary_index": {
				Type:     schema.TypeSet,
				Optional: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
						},
						"write_capacity": {
							Type:     schema.TypeInt,
							Required: true,
						},
						"read_capacity": {
							Type:     schema.TypeInt,
							Required: true,
						},
						"hash_key": {
							Type:     schema.TypeString,
							Required: true,
						},
						"range_key": {
							Type:     schema.TypeString,
							Optional: true,
						},
						"projection_type": {
							Type:     schema.TypeString,
							Required: true,
						},
						"non_key_attributes": {
							Type:     schema.TypeList,
							Optional: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
					},
				},
			},
			"stream_enabled": {
				Type:     schema.TypeBool,
				Optional: true,
				Computed: true,
			},
			"stream_view_type": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
				StateFunc: func(v interface{}) string {
					value := v.(string)
					return strings.ToUpper(value)
				},
				ValidateFunc: validateStreamViewType,
			},
			"stream_arn": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"stream_label": {
				Type:     schema.TypeString,
				Computed: true,
			},
			"tags": tagsSchema(),
		},
	}
}

func resourceAwsDynamoDbTableCreate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	hashKeyName := d.Get("hash_key").(string)

	req := &dynamodb.CreateTableInput{
		TableName: aws.String(d.Get("name").(string)),
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(int64(d.Get("read_capacity").(int))),
			WriteCapacityUnits: aws.Int64(int64(d.Get("write_capacity").(int))),
		},
		KeySchema: []*dynamodb.KeySchemaElement{
			{
				AttributeName: aws.String(hashKeyName),
				KeyType:       aws.String("HASH"),
			},
		},
	}

	if v, ok := d.GetOk("range_key"); ok {
		req.KeySchema = append(req.KeySchema, &dynamodb.KeySchemaElement{
			AttributeName: aws.String(v.(string)),
			KeyType:       aws.String("RANGE"),
		})
	}

	if v, ok := d.GetOk("attribute"); ok {
		attributes := []*dynamodb.AttributeDefinition{}
		attributeSet := v.(*schema.Set)

		for _, attribute := range attributeSet.List() {
			attr := attribute.(map[string]interface{})
			attributes = append(attributes, &dynamodb.AttributeDefinition{
				AttributeName: aws.String(attr["name"].(string)),
				AttributeType: aws.String(attr["type"].(string)),
			})
		}

		req.AttributeDefinitions = attributes
	}

	if v, ok := d.GetOk("local_secondary_index"); ok {
		lsiSet := v.(*schema.Set)
		localSecondaryIndexes := []*dynamodb.LocalSecondaryIndex{}
		for _, lsiObject := range lsiSet.List() {
			lsi := lsiObject.(map[string]interface{})

			projection := &dynamodb.Projection{
				ProjectionType: aws.String(lsi["projection_type"].(string)),
			}

			if lsi["projection_type"] == "INCLUDE" {
				non_key_attributes := []*string{}
				for _, attr := range lsi["non_key_attributes"].([]interface{}) {
					non_key_attributes = append(non_key_attributes, aws.String(attr.(string)))
				}
				projection.NonKeyAttributes = non_key_attributes
			}

			localSecondaryIndexes = append(localSecondaryIndexes, &dynamodb.LocalSecondaryIndex{
				IndexName: aws.String(lsi["name"].(string)),
				KeySchema: []*dynamodb.KeySchemaElement{
					{
						AttributeName: aws.String(hashKeyName),
						KeyType:       aws.String("HASH"),
					},
					{
						AttributeName: aws.String(lsi["range_key"].(string)),
						KeyType:       aws.String("RANGE"),
					},
				},
				Projection: projection,
			})
		}

		req.LocalSecondaryIndexes = localSecondaryIndexes
	}

	if v, ok := d.GetOk("global_secondary_index"); ok {
		globalSecondaryIndexes := []*dynamodb.GlobalSecondaryIndex{}
		gsiSet := v.(*schema.Set)
		for _, gsiObject := range gsiSet.List() {
			gsi := gsiObject.(map[string]interface{})
			gsiObject := expandDynamoDbGlobalSecondaryIndex(&gsi)
			globalSecondaryIndexes = append(globalSecondaryIndexes, &gsiObject)
		}
		req.GlobalSecondaryIndexes = globalSecondaryIndexes
	}

	if v, ok := d.GetOk("stream_enabled"); ok {
		req.StreamSpecification = &dynamodb.StreamSpecification{
			StreamEnabled:  aws.Bool(v.(bool)),
			StreamViewType: aws.String(d.Get("stream_view_type").(string)),
		}
	}

	var output *dynamodb.CreateTableOutput
	err := resource.Retry(1*time.Minute, func() *resource.RetryError {
		var err error
		output, err = conn.CreateTable(req)
		if err != nil {
			if isAWSErr(err, "ThrottlingException", "") {
				return resource.RetryableError(err)
			}
			if isAWSErr(err, "LimitExceededException", "can be created, updated, or deleted simultaneously") {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	d.SetId(*output.TableDescription.TableName)
	d.Set("arn", output.TableDescription.TableArn)

	if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
		return err
	}

	return resourceAwsDynamoDbTableUpdate(d, meta)
}

func resourceAwsDynamoDbTableUpdate(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	input := &dynamodb.UpdateTableInput{
		TableName: aws.String(d.Id()),
	}
	if d.HasChange("read_capacity") || d.HasChange("write_capacity") {
		input.ProvisionedThroughput = &dynamodb.ProvisionedThroughput{
			ReadCapacityUnits:  aws.Int64(int64(d.Get("read_capacity").(int))),
			WriteCapacityUnits: aws.Int64(int64(d.Get("write_capacity").(int))),
		}
	}

	if d.HasChange("stream_enabled") || d.HasChange("stream_view_type") {
		input.StreamSpecification = &dynamodb.StreamSpecification{
			StreamEnabled:  aws.Bool(d.Get("stream_enabled").(bool)),
			StreamViewType: aws.String(d.Get("stream_view_type").(string)),
		}
	}

	waitForGSI := false
	gsiName := ""
	if d.HasChange("global_secondary_index") {
		o, n := d.GetChange("global_secondary_index")
		oldSet := o.(*schema.Set)
		newSet := n.(*schema.Set)

		// Track old names so we can know which ones we need to just update based on
		// capacity changes, terraform appears to only diff on the set hash, not the
		// contents so we need to make sure we don't delete any indexes that we
		// just want to update the capacity for
		oldGsiNameSet := make(map[string]bool)
		newGsiNameSet := make(map[string]bool)

		for _, gsidata := range oldSet.List() {
			gsiName = gsidata.(map[string]interface{})["name"].(string)
			oldGsiNameSet[gsiName] = true
		}

		for _, gsidata := range newSet.List() {
			gsiName = gsidata.(map[string]interface{})["name"].(string)
			newGsiNameSet[gsiName] = true
		}

		// First determine what's new
		for _, newgsidata := range newSet.List() {
			updates := []*dynamodb.GlobalSecondaryIndexUpdate{}
			newGsiName := newgsidata.(map[string]interface{})["name"].(string)
			if _, exists := oldGsiNameSet[newGsiName]; !exists {
				attributes := []*dynamodb.AttributeDefinition{}
				gsidata := newgsidata.(map[string]interface{})
				gsi := expandDynamoDbGlobalSecondaryIndex(&gsidata)

				update := &dynamodb.GlobalSecondaryIndexUpdate{
					Create: &dynamodb.CreateGlobalSecondaryIndexAction{
						IndexName:             gsi.IndexName,
						KeySchema:             gsi.KeySchema,
						ProvisionedThroughput: gsi.ProvisionedThroughput,
						Projection:            gsi.Projection,
					},
				}
				updates = append(updates, update)

				// Hash key is required, range key isn't
				hashkey_type, err := getAttributeType(d, *gsi.KeySchema[0].AttributeName)
				if err != nil {
					return err
				}

				attributes = append(attributes, &dynamodb.AttributeDefinition{
					AttributeName: gsi.KeySchema[0].AttributeName,
					AttributeType: aws.String(hashkey_type),
				})

				// If there's a range key, there will be 2 elements in KeySchema
				if len(gsi.KeySchema) == 2 {
					rangekey_type, err := getAttributeType(d, *gsi.KeySchema[1].AttributeName)
					if err != nil {
						return err
					}

					attributes = append(attributes, &dynamodb.AttributeDefinition{
						AttributeName: gsi.KeySchema[1].AttributeName,
						AttributeType: aws.String(rangekey_type),
					})
				}

				input.AttributeDefinitions = attributes
				input.GlobalSecondaryIndexUpdates = updates

				waitForGSI = true
			}
		}

		for _, oldgsidata := range oldSet.List() {
			updates := []*dynamodb.GlobalSecondaryIndexUpdate{}
			oldGsiName := oldgsidata.(map[string]interface{})["name"].(string)
			if _, exists := newGsiNameSet[oldGsiName]; !exists {
				gsidata := oldgsidata.(map[string]interface{})
				log.Printf("[DEBUG] Deleting GSI %s", gsidata["name"].(string))
				update := &dynamodb.GlobalSecondaryIndexUpdate{
					Delete: &dynamodb.DeleteGlobalSecondaryIndexAction{
						IndexName: aws.String(gsidata["name"].(string)),
					},
				}
				updates = append(updates, update)

				input.GlobalSecondaryIndexUpdates = updates
			}
		}
	}

	err := resource.Retry(3*time.Minute, func() *resource.RetryError {
		_, err := conn.UpdateTable(input)
		if err != nil {
			if isAWSErr(err, "ValidationException", "cannot create or delete index while updating table IOPS") {
				return resource.RetryableError(err)
			}
			return resource.NonRetryableError(err)
		}
		return nil
	})
	if err != nil {
		return err
	}

	if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
		return fmt.Errorf("Error waiting for Dynamo DB Table update: %s", err)
	}

	if waitForGSI {
		if err := waitForGSIToBeActive(d.Id(), gsiName, conn); err != nil {
			return fmt.Errorf("Error waiting for Dynamo DB GSI to be active: %s", err)
		}
	}

	if d.HasChange("ttl") {
		if err := updateTimeToLive(d, conn); err != nil {
			log.Printf("[DEBUG] Error updating table TimeToLive: %s", err)
			return err
		}
	}

	if d.HasChange("tags") {
		if err := setTagsDynamoDb(conn, d); err != nil {
			return err
		}
	}

	return resourceAwsDynamoDbTableRead(d, meta)
}

func resourceAwsDynamoDbTableRead(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	result, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
		TableName: aws.String(d.Id()),
	})

	if err != nil {
		if isAWSErr(err, "ResourceNotFoundException", "") {
			log.Printf("[WARN] Dynamodb Table (%s) not found, error code (404)", d.Id())
			d.SetId("")
			return nil
		}
		return err
	}

	return flattenAwsDynamoDbTableResource(d, conn, result.Table)
}

func resourceAwsDynamoDbTableDelete(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	if err := waitForDynamoDbTableToBeActive(d.Id(), conn); err != nil {
		return fmt.Errorf("Error waiting for Dynamo DB Table update: %s", err)
	}

	log.Printf("[DEBUG] DynamoDB delete table: %s", d.Id())

	_, err := conn.DeleteTable(&dynamodb.DeleteTableInput{
		TableName: aws.String(d.Id()),
	})
	if err != nil {
		return err
	}

	stateConf := resource.StateChangeConf{
		Pending: []string{
			dynamodb.TableStatusActive,
			dynamodb.TableStatusDeleting,
		},
		Target:  []string{},
		Timeout: 1 * time.Minute,
		Refresh: func() (interface{}, string, error) {
			out, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(d.Id()),
			})
			if err != nil {
				if isAWSErr(err, "ResourceNotFoundException", "") {
					return nil, "", nil
				}

				return 42, "", err
			}
			table := out.Table

			return table, *table.TableStatus, nil
		},
	}
	_, err = stateConf.WaitForState()
	return err
}

func updateTimeToLive(d *schema.ResourceData, meta interface{}) error {
	conn := meta.(*AWSClient).dynamodbconn

	if ttl, ok := d.GetOk("ttl"); ok {
		timeToLiveSet := ttl.(*schema.Set)
		timeToLive := timeToLiveSet.List()[0].(map[string]interface{})

		_, err := conn.UpdateTimeToLive(&dynamodb.UpdateTimeToLiveInput{
			TableName: aws.String(d.Id()),
			TimeToLiveSpecification: &dynamodb.TimeToLiveSpecification{
				AttributeName: aws.String(timeToLive["attribute_name"].(string)),
				Enabled:       aws.Bool(timeToLive["enabled"].(bool)),
			},
		})
		if err != nil {
			// If ttl was not set within the .tf file before and has now been added we still run this command to update
			// But there has been no change so lets continue
			if isAWSErr(err, "ValidationException", "TimeToLive is already disabled") {
				return nil
			}
			log.Printf("[DEBUG] Error updating TimeToLive on table: %s", err)
			return err
		}

		if err := waitForTimeToLiveUpdateToBeCompleted(d.Id(), timeToLive["enabled"].(bool), conn); err != nil {
			return fmt.Errorf("Error waiting for Dynamo DB TimeToLive to be updated: %s", err)
		}
	}

	return nil
}

// Expanders + flatteners

func flattenAwsDynamoDbTableResource(d *schema.ResourceData, conn *dynamodb.DynamoDB, table *dynamodb.TableDescription) error {
	d.Set("write_capacity", table.ProvisionedThroughput.WriteCapacityUnits)
	d.Set("read_capacity", table.ProvisionedThroughput.ReadCapacityUnits)

	attributes := []interface{}{}
	for _, attrdef := range table.AttributeDefinitions {
		attribute := map[string]string{
			"name": *attrdef.AttributeName,
			"type": *attrdef.AttributeType,
		}
		attributes = append(attributes, attribute)
		log.Printf("[DEBUG] Added Attribute: %s", attribute["name"])
	}

	d.Set("attribute", attributes)
	d.Set("name", table.TableName)

	for _, attribute := range table.KeySchema {
		if *attribute.KeyType == "HASH" {
			d.Set("hash_key", attribute.AttributeName)
		}

		if *attribute.KeyType == "RANGE" {
			d.Set("range_key", attribute.AttributeName)
		}
	}

	lsiList := make([]map[string]interface{}, 0, len(table.LocalSecondaryIndexes))
	for _, lsiObject := range table.LocalSecondaryIndexes {
		lsi := map[string]interface{}{
			"name":            *lsiObject.IndexName,
			"projection_type": *lsiObject.Projection.ProjectionType,
		}

		for _, attribute := range lsiObject.KeySchema {

			if *attribute.KeyType == "RANGE" {
				lsi["range_key"] = *attribute.AttributeName
			}
		}
		nkaList := make([]string, len(lsiObject.Projection.NonKeyAttributes))
		for _, nka := range lsiObject.Projection.NonKeyAttributes {
			nkaList = append(nkaList, *nka)
		}
		lsi["non_key_attributes"] = nkaList

		lsiList = append(lsiList, lsi)
	}

	err := d.Set("local_secondary_index", lsiList)
	if err != nil {
		return err
	}

	gsiList := make([]map[string]interface{}, 0, len(table.GlobalSecondaryIndexes))
	for _, gsiObject := range table.GlobalSecondaryIndexes {
		gsi := map[string]interface{}{
			"write_capacity": *gsiObject.ProvisionedThroughput.WriteCapacityUnits,
			"read_capacity":  *gsiObject.ProvisionedThroughput.ReadCapacityUnits,
			"name":           *gsiObject.IndexName,
		}

		for _, attribute := range gsiObject.KeySchema {
			if *attribute.KeyType == "HASH" {
				gsi["hash_key"] = *attribute.AttributeName
			}

			if *attribute.KeyType == "RANGE" {
				gsi["range_key"] = *attribute.AttributeName
			}
		}

		gsi["projection_type"] = *(gsiObject.Projection.ProjectionType)

		nonKeyAttrs := make([]string, 0, len(gsiObject.Projection.NonKeyAttributes))
		for _, nonKeyAttr := range gsiObject.Projection.NonKeyAttributes {
			nonKeyAttrs = append(nonKeyAttrs, *nonKeyAttr)
		}
		gsi["non_key_attributes"] = nonKeyAttrs

		gsiList = append(gsiList, gsi)
		log.Printf("[DEBUG] Added GSI: %s - Read: %d / Write: %d", gsi["name"], gsi["read_capacity"], gsi["write_capacity"])
	}

	if table.StreamSpecification != nil {
		d.Set("stream_view_type", table.StreamSpecification.StreamViewType)
		d.Set("stream_enabled", table.StreamSpecification.StreamEnabled)
		d.Set("stream_arn", table.LatestStreamArn)
		d.Set("stream_label", table.LatestStreamLabel)
	}

	err = d.Set("global_secondary_index", gsiList)
	if err != nil {
		return err
	}

	d.Set("arn", table.TableArn)

	timeToLiveOutput, err := conn.DescribeTimeToLive(&dynamodb.DescribeTimeToLiveInput{
		TableName: aws.String(d.Id()),
	})
	if err != nil {
		return err
	}

	if timeToLiveOutput.TimeToLiveDescription != nil && timeToLiveOutput.TimeToLiveDescription.AttributeName != nil {
		timeToLiveList := []interface{}{
			map[string]interface{}{
				"attribute_name": *timeToLiveOutput.TimeToLiveDescription.AttributeName,
				"enabled":        (*timeToLiveOutput.TimeToLiveDescription.TimeToLiveStatus == dynamodb.TimeToLiveStatusEnabled),
			},
		}
		err := d.Set("ttl", timeToLiveList)
		if err != nil {
			return err
		}

		log.Printf("[DEBUG] Loaded TimeToLive data for DynamoDB table '%s'", d.Id())
	}

	tags, err := readTableTags(d.Get("arn").(string), conn)
	if err != nil {
		return err
	}
	d.Set("tags", tags)

	return nil
}

func expandDynamoDbGlobalSecondaryIndex(data *map[string]interface{}) dynamodb.GlobalSecondaryIndex {
	projection := &dynamodb.Projection{
		ProjectionType: aws.String((*data)["projection_type"].(string)),
	}

	if (*data)["projection_type"] == "INCLUDE" {
		non_key_attributes := []*string{}
		for _, attr := range (*data)["non_key_attributes"].([]interface{}) {
			non_key_attributes = append(non_key_attributes, aws.String(attr.(string)))
		}
		projection.NonKeyAttributes = non_key_attributes
	}

	writeCapacity := (*data)["write_capacity"].(int)
	readCapacity := (*data)["read_capacity"].(int)

	key_schema := []*dynamodb.KeySchemaElement{
		{
			AttributeName: aws.String((*data)["hash_key"].(string)),
			KeyType:       aws.String("HASH"),
		},
	}

	range_key_name := (*data)["range_key"]
	if range_key_name != "" {
		range_key_element := &dynamodb.KeySchemaElement{
			AttributeName: aws.String(range_key_name.(string)),
			KeyType:       aws.String("RANGE"),
		}

		key_schema = append(key_schema, range_key_element)
	}

	return dynamodb.GlobalSecondaryIndex{
		IndexName:  aws.String((*data)["name"].(string)),
		KeySchema:  key_schema,
		Projection: projection,
		ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
			WriteCapacityUnits: aws.Int64(int64(writeCapacity)),
			ReadCapacityUnits:  aws.Int64(int64(readCapacity)),
		},
	}
}

func getGlobalSecondaryIndex(indexName string, indexList []*dynamodb.GlobalSecondaryIndexDescription) (*dynamodb.GlobalSecondaryIndexDescription, error) {
	for _, gsi := range indexList {
		if *gsi.IndexName == indexName {
			return gsi, nil
		}
	}

	return &dynamodb.GlobalSecondaryIndexDescription{}, fmt.Errorf("Can't find a GSI by that name...")
}

func getAttributeType(d *schema.ResourceData, attributeName string) (string, error) {
	if attributedata, ok := d.GetOk("attribute"); ok {
		attributeSet := attributedata.(*schema.Set)
		for _, attribute := range attributeSet.List() {
			attr := attribute.(map[string]interface{})
			if attr["name"] == attributeName {
				return attr["type"].(string), nil
			}
		}
	}

	return "", fmt.Errorf("Unable to find an attribute named %s", attributeName)
}

func waitForGSIToBeActive(tableName string, gsiName string, conn *dynamodb.DynamoDB) error {
	stateConf := resource.StateChangeConf{
		Pending: []string{
			dynamodb.IndexStatusCreating,
			dynamodb.IndexStatusUpdating,
		},
		Target: []string{dynamodb.IndexStatusActive},
		Refresh: func() (interface{}, string, error) {
			result, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return 42, "", err
			}

			table := result.Table

			// Find index
			var targetGSI *dynamodb.GlobalSecondaryIndexDescription
			for _, gsi := range table.GlobalSecondaryIndexes {
				if *gsi.IndexName == gsiName {
					targetGSI = gsi
				}
			}

			if targetGSI != nil {
				return table, *targetGSI.IndexStatus, nil
			}

			return nil, "", nil
		},
	}
	_, err := stateConf.WaitForState()
	return err
}

func waitForDynamoDbTableToBeActive(tableName string, conn *dynamodb.DynamoDB) error {
	stateConf := resource.StateChangeConf{
		Pending: []string{dynamodb.TableStatusCreating, dynamodb.TableStatusUpdating},
		Target:  []string{dynamodb.TableStatusActive},
		Timeout: 2 * time.Minute,
		Refresh: func() (interface{}, string, error) {
			result, err := conn.DescribeTable(&dynamodb.DescribeTableInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return 42, "", err
			}

			return result, *result.Table.TableStatus, nil
		},
	}
	_, err := stateConf.WaitForState()

	return err

}

func waitForTimeToLiveUpdateToBeCompleted(tableName string, toEnable bool, conn *dynamodb.DynamoDB) error {
	pending := []string{
		dynamodb.TimeToLiveStatusEnabled,
		dynamodb.TimeToLiveStatusDisabling,
	}
	target := []string{dynamodb.TimeToLiveStatusDisabled}

	if toEnable {
		pending = []string{
			dynamodb.TimeToLiveStatusDisabled,
			dynamodb.TimeToLiveStatusEnabling,
		}
		target = []string{dynamodb.TimeToLiveStatusEnabled}
	}

	stateConf := resource.StateChangeConf{
		Pending: pending,
		Target:  target,
		Timeout: 10 * time.Second,
		Refresh: func() (interface{}, string, error) {
			result, err := conn.DescribeTimeToLive(&dynamodb.DescribeTimeToLiveInput{
				TableName: aws.String(tableName),
			})
			if err != nil {
				return 42, "", err
			}

			ttlDesc := result.TimeToLiveDescription

			return result, *ttlDesc.TimeToLiveStatus, nil
		},
	}

	_, err := stateConf.WaitForState()
	return err

}

func createTableTags(d *schema.ResourceData, conn *dynamodb.DynamoDB) error {
	_, err := conn.TagResource(&dynamodb.TagResourceInput{
		ResourceArn: aws.String(d.Get("arn").(string)),
		Tags:        tagsFromMapDynamoDb(d.Get("tags").(map[string]interface{})),
	})
	if err != nil {
		return fmt.Errorf("Error tagging dynamodb resource: %s", err)
	}
	return nil
}

func readTableTags(arn string, conn *dynamodb.DynamoDB) (map[string]string, error) {
	output, err := conn.ListTagsOfResource(&dynamodb.ListTagsOfResourceInput{
		ResourceArn: aws.String(arn),
	})
	if err != nil {
		return nil, fmt.Errorf("Error reading tags from dynamodb resource: %s", err)
	}

	result := tagsToMapDynamoDb(output.Tags)

	// TODO Read NextToken if avail

	return result, nil
}
